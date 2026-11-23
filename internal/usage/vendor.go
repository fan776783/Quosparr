package usage

import (
	"sort"
	"strings"
	"time"

	"github.com/licoy/quosparr/internal/pricing"
	"github.com/licoy/quosparr/internal/usage/decimal"
)

// vendorPricingProviders are the official snapshot providers matched by
// pricing.Snapshot.LookupVendor, in stable order for version stamps.
var vendorPricingProviders = []string{
	"alibaba",
	"anthropic",
	"deepseek",
	"google",
	"minimax",
	"moonshotai",
	"openai",
	"xai",
	"zhipuai",
}

// PricedEvent is one usage unit (a message or session) already mapped to
// usage.Totals and optionally priced against a vendor snapshot.
type PricedEvent struct {
	Time           time.Time
	Model, Project string
	Session        string
	Totals         Totals
	USD            *decimal.Decimal
	UnpricedReason string
}

type eventBucket struct {
	totals         Totals
	sessions       map[string]struct{}
	usd            *decimal.Decimal
	unpricedTokens int64
}

func (b *eventBucket) add(ev PricedEvent) {
	b.totals.TotalTokens += ev.Totals.TotalTokens
	b.totals.UncachedInputTokens += ev.Totals.UncachedInputTokens
	b.totals.CachedInputTokens += ev.Totals.CachedInputTokens
	b.totals.OutputTokens += ev.Totals.OutputTokens
	b.totals.Turns += ev.Totals.Turns
	if ev.Session != "" {
		if b.sessions == nil {
			b.sessions = map[string]struct{}{}
		}
		if _, ok := b.sessions[ev.Session]; !ok {
			b.sessions[ev.Session] = struct{}{}
			b.totals.Threads++
		}
	} else {
		b.totals.Threads += ev.Totals.Threads
	}
	if ev.USD != nil {
		if b.usd == nil {
			v := *ev.USD
			b.usd = &v
		} else {
			sum := b.usd.Add(*ev.USD)
			b.usd = &sum
		}
	} else {
		b.unpricedTokens += ev.Totals.TotalTokens
	}
}

// SummarizePricedEvents aggregates priced events in [w.Start, w.End) into a
// Summary, grouping daily rows by loc's calendar date. Zero-token events are
// skipped.
func SummarizePricedEvents(events []PricedEvent, w Window, loc *time.Location, client, pricingVersion string, now time.Time) Summary {
	if loc == nil {
		loc = time.Local
	}
	var agg eventBucket
	byDay := map[string]*eventBucket{}
	byModel := map[string]*eventBucket{}
	byProject := map[string]*eventBucket{}
	reasons := map[string]struct{}{}

	addBucket := func(m map[string]*eventBucket, key string, ev PricedEvent) {
		b := m[key]
		if b == nil {
			b = &eventBucket{}
			m[key] = b
		}
		b.add(ev)
	}

	for _, ev := range events {
		if ev.Time.Before(w.Start) || !ev.Time.Before(w.End) {
			continue
		}
		if ev.Totals.TotalTokens <= 0 {
			continue
		}
		model := ev.Model
		if model == "" {
			model = "unknown"
		}
		project := ev.Project
		if project == "" {
			project = UnknownProject
		}
		day := ev.Time.In(loc).Format(time.DateOnly)
		agg.add(ev)
		addBucket(byDay, day, ev)
		addBucket(byModel, model, ev)
		addBucket(byProject, project, ev)
		if ev.USD == nil && ev.UnpricedReason != "" {
			reasons[ev.UnpricedReason] = struct{}{}
		}
	}

	days := make([]string, 0, len(byDay))
	for d := range byDay {
		days = append(days, d)
	}
	sort.Strings(days)
	daily := make([]DailyRow, 0, len(days))
	for _, d := range days {
		b := byDay[d]
		daily = append(daily, DailyRow{
			Date:           d,
			Totals:         b.totals,
			EstimatedUSD:   b.usd,
			UnpricedTokens: b.unpricedTokens,
		})
	}

	models := sortedEventRows(byModel)
	projects := sortedEventRows(byProject)

	clients := []BreakdownRow{}
	if agg.totals.TotalTokens > 0 && client != "" {
		clients = append(clients, BreakdownRow{
			Name:           client,
			Totals:         agg.totals,
			EstimatedUSD:   agg.usd,
			UnpricedTokens: agg.unpricedTokens,
		})
	}

	warnings := make([]string, 0, len(reasons))
	for reason := range reasons {
		warnings = append(warnings, reason)
	}
	sort.Strings(warnings)

	source, confidence := SourceUnavailable, ConfidencePriced
	if agg.totals.TotalTokens > 0 {
		source = SourceLocalSessions
	}
	switch {
	case agg.totals.TotalTokens == 0:
		confidence = ConfidenceUnavailable
	case agg.usd == nil:
		confidence = ConfidenceTokensOnly
	case agg.unpricedTokens > 0:
		confidence = ConfidencePartiallyPriced
	}

	return Summary{
		Source:         source,
		Confidence:     confidence,
		Window:         w,
		EstimatedUSD:   agg.usd,
		UnpricedTokens: agg.unpricedTokens,
		Totals:         agg.totals,
		DailyRows:      daily,
		ModelRows:      models,
		ProjectRows:    projects,
		ClientRows:     clients,
		Warnings:       warnings,
		PricingVersion: pricingVersion,
		CalculatedAt:   now,
	}
}

func sortedEventRows(m map[string]*eventBucket) []BreakdownRow {
	rows := make([]BreakdownRow, 0, len(m))
	for name, b := range m {
		rows = append(rows, BreakdownRow{
			Name:           name,
			Totals:         b.totals,
			EstimatedUSD:   b.usd,
			UnpricedTokens: b.unpricedTokens,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Totals.TotalTokens != rows[j].Totals.TotalTokens {
			return rows[i].Totals.TotalTokens > rows[j].Totals.TotalTokens
		}
		return vendorNameLess(rows[i].Name, rows[j].Name)
	})
	return rows
}

func vendorNameLess(a, b string) bool {
	if la, lb := strings.ToLower(a), strings.ToLower(b); la != lb {
		return la < lb
	}
	return a < b
}

// VendorCost prices one event against m. When prompt < 0 (prompt length
// unknown, as with session-level totals), it uses m.Rate directly. Otherwise
// it applies context-tier selection matching gemini.sessionCost, and returns
// false when a legacy long-context model lacks tier boundaries, a tier is not
// a context tier, prompt equals a tier boundary, or any used token part lacks a
// rate (including cache writes without a cache_write rate).
func VendorCost(m pricing.Model, input, cacheRead, cacheWrite, output, prompt int64) (decimal.Decimal, bool) {
	rate := m.Rate
	if prompt >= 0 {
		if m.LegacyLong != nil && len(m.Tiers) == 0 {
			return decimal.Decimal{}, false
		}
		chosen := int64(-1)
		for _, tier := range m.Tiers {
			switch {
			case tier.Type != "context" || prompt == tier.Size:
				return decimal.Decimal{}, false
			case prompt > tier.Size && tier.Size > chosen:
				rate, chosen = tier.Rate, tier.Size
			}
		}
	}
	parts := []struct {
		tokens int64
		rate   *decimal.Decimal
	}{
		{input, rate.Input},
		{cacheRead, rate.CacheRead},
		{cacheWrite, rate.CacheWrite},
		{output, rate.Output},
	}
	sum := decimal.Decimal{}
	for _, part := range parts {
		if part.tokens <= 0 {
			continue
		}
		if part.rate == nil {
			return decimal.Decimal{}, false
		}
		sum = sum.Add(decimal.FromInt64(part.tokens).Mul(*part.rate))
	}
	return sum.Div(decimal.FromInt(1_000_000)), true
}

// VendorPricingVersion joins prefix with the snapshot versions of every vendor
// provider that LookupVendor can resolve.
func VendorPricingVersion(prefix string, snap pricing.Snapshot) string {
	version := prefix
	for _, provider := range vendorPricingProviders {
		if v := snap.ProviderVersion(provider); v != "" {
			if version == "" {
				version = provider + ":" + v
			} else {
				version += "+" + provider + ":" + v
			}
		}
	}
	return version
}

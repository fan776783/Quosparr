package usage

import (
	"slices"
	"testing"
	"time"

	"github.com/licoy/quosparr/internal/pricing"
	"github.com/licoy/quosparr/internal/usage/decimal"
)

func TestVendorCostTiersAndMissingRates(t *testing.T) {
	d := func(s string) *decimal.Decimal { v := decimal.MustParse(s); return &v }

	tiered := pricing.Model{
		ProviderID: "anthropic",
		ID:         "claude-opus-5-5",
		Rate:       pricing.Rate{Input: d("5"), Output: d("25"), CacheRead: d("0.5"), CacheWrite: d("6.25")},
		Tiers: []pricing.Tier{
			{Size: 200_000, Type: "context", Rate: pricing.Rate{Input: d("10"), Output: d("37.5"), CacheRead: d("1"), CacheWrite: d("12.5")}},
		},
	}

	// Below tier threshold uses base rate.
	cost, ok := VendorCost(tiered, 1000, 2000, 500, 100, 3500)
	if !ok || cost.String() != "0.011625" {
		t.Fatalf("base tier = %v, %v", cost, ok)
	}

	// Above tier threshold uses tier rate.
	cost, ok = VendorCost(tiered, 200_000, 1000, 0, 100, 201_000)
	if !ok || cost.String() != "2.00475" {
		t.Fatalf("high tier = %v, %v", cost, ok)
	}

	// Prompt unknown (< 0) uses base rate even when tiers or legacyLong are present.
	cost, ok = VendorCost(tiered, 200_000, 1000, 0, 100, -1)
	if !ok || cost.String() != "1.003" {
		t.Fatalf("unknown prompt = %v, %v", cost, ok)
	}

	// Exact boundary is ambiguous -> false.
	if _, ok := VendorCost(tiered, 200_000, 0, 0, 100, 200_000); ok {
		t.Fatal("exact tier boundary should be unpriced")
	}

	// Cache write present but cache_write rate missing -> false (never falls back to input).
	noCacheWrite := pricing.Model{
		ProviderID: "anthropic",
		ID:         "claude-no-cw",
		Rate:       pricing.Rate{Input: d("5"), Output: d("25"), CacheRead: d("0.5")},
	}
	if _, ok := VendorCost(noCacheWrite, 100, 0, 50, 10, 150); ok {
		t.Fatal("missing cache_write rate should be unpriced")
	}
	if _, ok := VendorCost(noCacheWrite, 100, 0, 0, 10, 100); !ok {
		t.Fatal("zero cache_write tokens should succeed without cache_write rate")
	}

	// LegacyLong without tiers fails when prompt >= 0, succeeds when prompt < 0.
	legacy := pricing.Model{
		ProviderID: "openai",
		ID:         "gpt-legacy",
		Rate:       pricing.Rate{Input: d("2"), Output: d("10")},
		LegacyLong: &pricing.Rate{Input: d("4"), Output: d("20")},
	}
	if _, ok := VendorCost(legacy, 100, 0, 0, 10, 100); ok {
		t.Fatal("legacyLong with known prompt should be unpriced")
	}
	if c, ok := VendorCost(legacy, 100, 0, 0, 10, -1); !ok || c.String() != "0.0003" {
		t.Fatalf("legacyLong with unknown prompt = %v, %v", c, ok)
	}
}

func TestSummarizePricedEvents(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*3600)
	start := time.Date(2026, 11, 20, 0, 0, 0, 0, loc)
	end := time.Date(2026, 11, 22, 0, 0, 0, 0, loc)
	w := Window{Start: start, End: end}
	now := end

	usd1 := decimal.MustParse("0.50")
	usd2 := decimal.MustParse("1.25")
	events := []PricedEvent{
		// Before window -> excluded
		{Time: start.Add(-time.Second), Model: "claude-opus-5-5", Project: "alpha", Session: "s0", Totals: Totals{TotalTokens: 100, UncachedInputTokens: 100, Turns: 1}, USD: &usd1},
		// Exactly at end -> excluded
		{Time: end, Model: "claude-opus-5-5", Project: "alpha", Session: "s0", Totals: Totals{TotalTokens: 100, UncachedInputTokens: 100, Turns: 1}, USD: &usd1},
		// Zero-token event -> excluded
		{Time: start.Add(time.Hour), Model: "claude-opus-5-5", Project: "alpha", Session: "s-empty", Totals: Totals{TotalTokens: 0}},
		// Exactly at start -> included; 2026-11-20 00:00 +08:00 (2026-11-19 16:00 UTC)
		{Time: start, Model: "claude-opus-5-5", Project: "alpha", Session: "s1", Totals: Totals{TotalTokens: 300, UncachedInputTokens: 200, OutputTokens: 100, Turns: 1}, USD: &usd1},
		// Same session, next local day
		{Time: start.Add(24 * time.Hour), Model: "gpt-5.5", Project: "alpha", Session: "s1", Totals: Totals{TotalTokens: 400, UncachedInputTokens: 250, CachedInputTokens: 50, OutputTokens: 100, Turns: 1}, USD: &usd2},
		// Unknown model on day 2 in beta project -> unpriced
		{Time: start.Add(25 * time.Hour), Model: "swe-2-medium", Project: "beta", Session: "s2", Totals: Totals{TotalTokens: 500, UncachedInputTokens: 400, OutputTokens: 100, Turns: 1}, UnpricedReason: "unknown-model:swe-2-medium"},
		// Duplicate warning reason from another model event
		{Time: start.Add(26 * time.Hour), Model: "swe-2-medium", Project: "beta", Session: "s2", Totals: Totals{TotalTokens: 100, UncachedInputTokens: 80, OutputTokens: 20, Turns: 1}, UnpricedReason: "unknown-model:swe-2-medium"},
		// Missing price rule warning
		{Time: start.Add(26 * time.Hour), Model: "compactor", Project: "beta", Session: "s3", Totals: Totals{TotalTokens: 50, UncachedInputTokens: 40, OutputTokens: 10, Turns: 1}, UnpricedReason: "missing-price-rule:compactor"},
	}

	sum := SummarizePricedEvents(events, w, loc, "Devin", "devin-v1", now)
	if sum.Source != SourceLocalSessions || sum.Confidence != ConfidencePartiallyPriced {
		t.Fatalf("source=%s confidence=%s", sum.Source, sum.Confidence)
	}
	if sum.EstimatedUSD == nil || sum.EstimatedUSD.String() != "1.75" || sum.UnpricedTokens != 650 {
		t.Fatalf("usd=%v unpriced=%d", sum.EstimatedUSD, sum.UnpricedTokens)
	}
	wantTotals := Totals{TotalTokens: 1350, UncachedInputTokens: 970, CachedInputTokens: 50, OutputTokens: 330, Turns: 5, Threads: 3}
	if sum.Totals != wantTotals {
		t.Fatalf("totals = %+v, want %+v", sum.Totals, wantTotals)
	}
	if len(sum.DailyRows) != 2 || sum.DailyRows[0].Date != "2026-11-20" || sum.DailyRows[1].Date != "2026-11-21" {
		t.Fatalf("daily = %+v", sum.DailyRows)
	}
	if sum.DailyRows[0].Totals.Threads != 1 || sum.DailyRows[1].Totals.Threads != 3 {
		t.Fatalf("daily threads = %d, %d", sum.DailyRows[0].Totals.Threads, sum.DailyRows[1].Totals.Threads)
	}
	if len(sum.ModelRows) != 4 || sum.ModelRows[0].Name != "swe-2-medium" || sum.ModelRows[1].Name != "gpt-5.5" || sum.ModelRows[2].Name != "claude-opus-5-5" || sum.ModelRows[3].Name != "compactor" {
		t.Fatalf("models = %+v", sum.ModelRows)
	}
	if len(sum.ProjectRows) != 2 || sum.ProjectRows[0].Name != "alpha" || sum.ProjectRows[1].Name != "beta" {
		t.Fatalf("projects = %+v", sum.ProjectRows)
	}
	if len(sum.ClientRows) != 1 || sum.ClientRows[0].Name != "Devin" {
		t.Fatalf("clients = %+v", sum.ClientRows)
	}
	wantWarnings := []string{"missing-price-rule:compactor", "unknown-model:swe-2-medium"}
	if !slices.Equal(sum.Warnings, wantWarnings) {
		t.Fatalf("warnings = %v, want %v", sum.Warnings, wantWarnings)
	}
}

func TestVendorPricingVersion(t *testing.T) {
	d := decimal.MustParse("1")
	snap, err := pricing.NewSnapshot([]pricing.Model{
		{ProviderID: "anthropic", ID: "claude-opus-5-5", Rate: pricing.Rate{Input: &d, Output: &d}},
		{ProviderID: "moonshotai", ID: "kimi-k3", Rate: pricing.Rate{Input: &d, Output: &d}},
		{ProviderID: "openrouter", ID: "ignored", Rate: pricing.Rate{Input: &d, Output: &d}},
	})
	if err != nil {
		t.Fatal(err)
	}
	v1 := VendorPricingVersion("droid-snapshot", snap)
	v2 := VendorPricingVersion("droid-snapshot", pricing.Snapshot{})
	if v1 == v2 {
		t.Fatalf("expected different versions, got %q", v1)
	}
	if v2 != "droid-snapshot" {
		t.Fatalf("empty snapshot version = %q", v2)
	}
}

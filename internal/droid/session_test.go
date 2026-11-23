package droid

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/licoy/quosparr/internal/pricing"
	"github.com/licoy/quosparr/internal/usage"
	"github.com/licoy/quosparr/internal/usage/decimal"
)

func testDroidPricing(t *testing.T) pricing.Snapshot {
	t.Helper()
	d := func(s string) *decimal.Decimal { v := decimal.MustParse(s); return &v }
	snap, err := pricing.NewSnapshot([]pricing.Model{
		{
			ProviderID: "anthropic",
			ID:         "claude-opus-5-5",
			Rate:       pricing.Rate{Input: d("5"), Output: d("25"), CacheRead: d("0.5"), CacheWrite: d("6.25")},
			Tiers:      []pricing.Tier{{Size: 200_000, Type: "context", Rate: pricing.Rate{Input: d("10"), Output: d("37.5")}}},
		},
		{
			ProviderID: "moonshotai",
			ID:         "kimi-k3",
			Rate:       pricing.Rate{Input: d("1"), Output: d("4"), CacheRead: d("0.2")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

func writeDroidSession(t *testing.T, path string, body string, mtime time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if !mtime.IsZero() {
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSessionScannerMissingAndCanceled(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	w := usage.Window{Start: now.Add(-24 * time.Hour), End: now}

	missing := SessionScanner{Dir: filepath.Join(t.TempDir(), "missing"), Location: time.UTC}
	sum, err := missing.ScanCost(context.Background(), w, now)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Source != usage.SourceUnavailable || sum.Confidence != usage.ConfidenceUnavailable || sum.Totals.TotalTokens != 0 {
		t.Fatalf("missing summary = %+v", sum)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := missing.ScanCost(ctx, w, now); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled err = %v", err)
	}
}

func TestSessionScannerCountsPricingAndBoundaries(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*3600)
	start := time.Date(2026, 11, 20, 0, 0, 0, 0, loc)
	end := time.Date(2026, 11, 22, 0, 0, 0, 0, loc)
	w := usage.Window{Start: start, End: end}
	dir := filepath.Join(t.TempDir(), ".factory", "sessions")
	snap := testDroidPricing(t)

	// 1. Valid priced Anthropic session using tokenUsage (not inclusiveTokenUsage),
	//    with .jsonl mtime pulling it into the window.
	//    Cost at base Rate (prompt unknown):
	//    input=300_000 * 5 + cacheRead=20_000 * 0.5 + cacheWrite=10_000 * 6.25 + output=4_000 * 25
	//    = (1_500_000 + 10_000 + 62_500 + 100_000) / 1_000_000 = 1.6725
	writeDroidSession(t, filepath.Join(dir, "proj-alpha", "s1.settings.json"), `{
		"model": "claude-opus-5-5-medium",
		"tokenUsage": {"inputTokens": 300000, "outputTokens": 4000, "cacheCreationTokens": 10000, "cacheReadTokens": 20000, "thinkingTokens": 500},
		"inclusiveTokenUsage": {"inputTokens": 9999999, "outputTokens": 9999999}
	}`, start.Add(-48*time.Hour))
	writeDroidSession(t, filepath.Join(dir, "proj-alpha", "s1.jsonl"), "{}\n", start.Add(2*time.Hour))

	// 2. custom: BYOK model must be excluded from both cost and tokens.
	writeDroidSession(t, filepath.Join(dir, "proj-alpha", "s-custom.settings.json"), `{
		"model": "custom:my-local-model",
		"tokenUsage": {"inputTokens": 5000, "outputTokens": 1000}
	}`, start.Add(3*time.Hour))

	// 3. Unknown model on next local day.
	writeDroidSession(t, filepath.Join(dir, "proj-beta", "s2.settings.json"), `{
		"model": "swe-2-medium",
		"tokenUsage": {"inputTokens": 1000, "outputTokens": 200}
	}`, start.Add(25*time.Hour))

	// 4. Missing cache_write rate when cacheCreationTokens > 0 -> unpriced.
	writeDroidSession(t, filepath.Join(dir, "proj-beta", "s3.settings.json"), `{
		"model": "kimi-k3",
		"tokenUsage": {"inputTokens": 100, "cacheCreationTokens": 50, "outputTokens": 10}
	}`, start.Add(26*time.Hour))

	// 5. Zero-token session -> excluded.
	writeDroidSession(t, filepath.Join(dir, "proj-beta", "s-zero.settings.json"), `{
		"model": "kimi-k3",
		"tokenUsage": {"inputTokens": 0, "outputTokens": 0}
	}`, start.Add(4*time.Hour))

	// 6. Outside window (End is exclusive) -> excluded.
	writeDroidSession(t, filepath.Join(dir, "proj-beta", "s-out.settings.json"), `{
		"model": "kimi-k3",
		"tokenUsage": {"inputTokens": 1000, "outputTokens": 100}
	}`, end)

	// 7. Malformed JSON -> skipped without error.
	writeDroidSession(t, filepath.Join(dir, "proj-beta", "s-bad.settings.json"), `{not-json`, start.Add(5*time.Hour))

	scanner := SessionScanner{
		Dir:             dir,
		Location:        loc,
		PricingSnapshot: func() pricing.Snapshot { return snap },
	}
	sum, err := scanner.ScanCost(context.Background(), w, end)
	if err != nil {
		t.Fatal(err)
	}

	if sum.Source != usage.SourceLocalSessions || sum.Confidence != usage.ConfidencePartiallyPriced {
		t.Fatalf("source=%s confidence=%s", sum.Source, sum.Confidence)
	}
	if sum.EstimatedUSD == nil || sum.EstimatedUSD.String() != "1.6725" {
		t.Fatalf("usd = %v, want 1.6725", sum.EstimatedUSD)
	}
	wantTotals := usage.Totals{
		UncachedInputTokens: 300_000 + 10_000 + 1_000 + 100 + 50,
		CachedInputTokens:   20_000,
		OutputTokens:        4_000 + 200 + 10,
		TotalTokens:         335_360,
		Turns:               0,
		Threads:             3,
	}
	if sum.Totals != wantTotals {
		t.Fatalf("totals = %+v, want %+v", sum.Totals, wantTotals)
	}
	if sum.UnpricedTokens != 1360 {
		t.Fatalf("unpriced = %d, want 1360", sum.UnpricedTokens)
	}
	wantWarnings := []string{"missing-price-rule:kimi-k3", "unknown-model:swe-2-medium"}
	if !slices.Equal(sum.Warnings, wantWarnings) {
		t.Fatalf("warnings = %v, want %v", sum.Warnings, wantWarnings)
	}
	if len(sum.ClientRows) != 1 || sum.ClientRows[0].Name != "Droid" {
		t.Fatalf("clients = %+v", sum.ClientRows)
	}
	if len(sum.ProjectRows) != 2 || sum.ProjectRows[0].Name != "proj-alpha" || sum.ProjectRows[1].Name != "proj-beta" {
		t.Fatalf("projects = %+v", sum.ProjectRows)
	}
	if len(sum.DailyRows) != 2 || sum.DailyRows[0].Date != "2026-11-20" || sum.DailyRows[1].Date != "2026-11-21" {
		t.Fatalf("daily = %+v", sum.DailyRows)
	}

	events, ver, err := scanner.ScanEvents(context.Background(), w, end)
	if err != nil {
		t.Fatal(err)
	}
	fromEvents := usage.SummarizePricedEvents(events, w, loc, SessionClientName, ver, end)
	if fromEvents.Totals != sum.Totals || fromEvents.EstimatedUSD.String() != sum.EstimatedUSD.String() || fromEvents.PricingVersion != sum.PricingVersion {
		t.Fatalf("ScanEvents summary mismatch: got %+v, want %+v", fromEvents, sum)
	}
}

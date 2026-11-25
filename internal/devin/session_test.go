package devin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
	"github.com/licoy/quosparr/internal/pricing"
	"github.com/licoy/quosparr/internal/sqliteruntime"
	"github.com/licoy/quosparr/internal/usage"
	"github.com/licoy/quosparr/internal/usage/decimal"
)

func testDevinPricing(t *testing.T) pricing.Snapshot {
	t.Helper()
	d := func(s string) *decimal.Decimal { v := decimal.MustParse(s); return &v }
	snap, err := pricing.NewSnapshot([]pricing.Model{
		{
			ProviderID: "anthropic",
			ID:         "claude-opus-5-5",
			Rate:       pricing.Rate{Input: d("5"), Output: d("25"), CacheRead: d("0.5"), CacheWrite: d("6.25")},
			Tiers:      []pricing.Tier{{Size: 200_000, Type: "context", Rate: pricing.Rate{Input: d("10"), Output: d("37.5"), CacheRead: d("1"), CacheWrite: d("12.5")}}},
		},
		{
			ProviderID: "anthropic",
			ID:         "claude-fable-5-2",
			Rate:       pricing.Rate{Input: d("3"), Output: d("15"), CacheRead: d("0.3")}, // missing cache_write
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

func TestDevinSessionScannerMissingAndCanceled(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	w := usage.Window{Start: now.Add(-24 * time.Hour), End: now}

	missing := SessionScanner{DataDir: t.TempDir(), Location: time.UTC}
	sum, err := missing.ScanCost(context.Background(), w, now)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Source != usage.SourceUnavailable || sum.Confidence != usage.ConfidenceUnavailable {
		t.Fatalf("missing summary = %+v", sum)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := missing.ScanCost(ctx, w, now); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled err = %v", err)
	}
}

func TestDevinSessionScannerCountsTiersFallbacksAndBoundaries(t *testing.T) {
	if err := sqliteruntime.Load(); err != nil {
		t.Fatal(err)
	}
	loc := time.FixedZone("UTC+8", 8*3600)
	start := time.Date(2026, 11, 20, 0, 0, 0, 0, loc)
	end := time.Date(2026, 11, 22, 0, 0, 0, 0, loc)
	w := usage.Window{Start: start, End: end}

	dataDir := t.TempDir()
	dbPath := SessionDBPath(dataDir)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	db, err := sqlite.Open(ctx, dbPath, sqlite.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, ddl := range []string{
		`CREATE TABLE sessions (id TEXT PRIMARY KEY, working_directory TEXT)`,
		`CREATE TABLE message_nodes (session_id TEXT, chat_message TEXT, created_at INTEGER)`,
	} {
		if _, err := db.Execute(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	insertSession := func(id, cwd string) {
		if _, err := db.Execute(ctx, `INSERT INTO sessions(id, working_directory) VALUES (?, ?)`, id, cwd); err != nil {
			t.Fatal(err)
		}
	}
	insertMsg := func(sessionID, msg string, createdAt int64) {
		if _, err := db.Execute(ctx, `INSERT INTO message_nodes(session_id, chat_message, created_at) VALUES (?, ?, ?)`, sessionID, msg, createdAt); err != nil {
			t.Fatal(err)
		}
	}

	insertSession("s1", "/Users/dev/work/alpha")
	insertSession("s2", "/Users/dev/work/beta")

	// 1. Base-tier assistant call on day 1 (metadata.created_at takes precedence over column created_at).
	//    Cost: 1000*5 + 2000*0.5 + 500*6.25 + 100*25 = (5000+1000+3125+2500)/1e6 = 0.011625
	insertMsg("s1", `{"message_id":"cost-1","role":"assistant","metadata":{"generation_model":"claude-opus-5-5-medium","created_at":"`+start.Add(time.Hour).Format(time.RFC3339)+`","metrics":{"input_tokens":1000,"output_tokens":100,"cache_read_tokens":2000,"cache_creation_tokens":500}}}`, start.Add(-12*time.Hour).Unix())
	insertMsg("s1", `{"message_id":"cost-1","role":"assistant","metadata":{"generation_model":"claude-opus-5-5-medium","created_at":"`+start.Add(time.Hour).Format(time.RFC3339)+`","metrics":{"input_tokens":1000,"output_tokens":100,"cache_read_tokens":2000,"cache_creation_tokens":500}}}`, start.Add(-12*time.Hour).Unix())

	// 2. Context-tier assistant call (> 200k prompt) on day 2 in same session.
	//    prompt = 210_000 + 10_000 + 0 = 220_000 > 200_000
	//    Cost: 210_000*10 + 10_000*1 + 1000*37.5 = (2_100_000+10_000+37_500)/1e6 = 2.1475
	insertMsg("s1", `{"role":"assistant","metadata":{"request_id":"req-2","generation_model":"claude-opus-5-5-medium","metrics":{"input_tokens":210000,"output_tokens":1000,"cache_read_tokens":10000,"cache_creation_tokens":0}}}`, start.Add(25*time.Hour).Unix())
	insertMsg("s1", `{"role":"assistant","metadata":{"request_id":"req-2","generation_model":"claude-opus-5-5-medium","metrics":{"input_tokens":210000,"output_tokens":1000,"cache_read_tokens":10000,"cache_creation_tokens":0}}}`, start.Add(25*time.Hour).Unix())

	// 3. Unknown model ("swe-2-medium") in session s2 -> unpriced.
	insertMsg("s2", `{"role":"assistant","metadata":{"generation_model":"swe-2-medium","metrics":{"input_tokens":500,"output_tokens":50}}}`, start.Add(26*time.Hour).Unix())

	// 4. Missing cache_write rate when cache_creation_tokens > 0 -> unpriced.
	insertMsg("s2", `{"role":"assistant","metadata":{"generation_model":"claude-fable-5-2-high","metrics":{"input_tokens":100,"output_tokens":20,"cache_creation_tokens":30}}}`, start.Add(27*time.Hour).Unix())

	// 5. User message -> skipped.
	insertMsg("s1", `{"role":"user","metadata":{"generation_model":"claude-opus-5-5","metrics":{"input_tokens":999,"output_tokens":999}}}`, start.Add(2*time.Hour).Unix())

	// 6. Assistant message without metrics -> skipped.
	insertMsg("s1", `{"role":"assistant","metadata":{"generation_model":"claude-opus-5-5"}}`, start.Add(2*time.Hour).Unix())

	// 7. Assistant message at End (exclusive) -> excluded.
	insertMsg("s2", `{"role":"assistant","metadata":{"generation_model":"claude-opus-5-5","created_at":"`+end.Format(time.RFC3339)+`","metrics":{"input_tokens":999,"output_tokens":999}}}`, end.Unix())

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	snap := testDevinPricing(t)
	scanner := SessionScanner{
		DataDir:         dataDir,
		Location:        loc,
		PricingSnapshot: func() pricing.Snapshot { return snap },
	}
	sum, err := scanner.ScanCost(ctx, w, end)
	if err != nil {
		t.Fatal(err)
	}

	if sum.Source != usage.SourceLocalSessions || sum.Confidence != usage.ConfidencePartiallyPriced {
		t.Fatalf("source=%s confidence=%s", sum.Source, sum.Confidence)
	}
	// Total USD: 0.011625 + 2.1475 = 2.159125
	if sum.EstimatedUSD == nil || sum.EstimatedUSD.String() != "2.159125" {
		t.Fatalf("usd = %v, want 2.159125", sum.EstimatedUSD)
	}
	wantTotals := usage.Totals{
		UncachedInputTokens: 1000 + 500 + 210_000 + 500 + 100 + 30,
		CachedInputTokens:   2000 + 10_000,
		OutputTokens:        100 + 1000 + 50 + 20,
		TotalTokens:         225_300,
		Turns:               4,
		Threads:             2,
	}
	if sum.Totals != wantTotals {
		t.Fatalf("totals = %+v, want %+v", sum.Totals, wantTotals)
	}
	if sum.UnpricedTokens != 700 {
		t.Fatalf("unpriced = %d, want 700", sum.UnpricedTokens)
	}
	wantWarnings := []string{"missing-price-rule:claude-fable-5-2-high", "unknown-model:swe-2-medium"}
	if !slices.Equal(sum.Warnings, wantWarnings) {
		t.Fatalf("warnings = %v, want %v", sum.Warnings, wantWarnings)
	}
	if len(sum.ClientRows) != 1 || sum.ClientRows[0].Name != "Devin" {
		t.Fatalf("clients = %+v", sum.ClientRows)
	}
	if len(sum.ProjectRows) != 2 || sum.ProjectRows[0].Name != "alpha" || sum.ProjectRows[1].Name != "beta" {
		t.Fatalf("projects = %+v", sum.ProjectRows)
	}
	if len(sum.DailyRows) != 2 || sum.DailyRows[0].Date != "2026-11-20" || sum.DailyRows[1].Date != "2026-11-21" {
		t.Fatalf("daily = %+v", sum.DailyRows)
	}

	events, ver, err := scanner.ScanEvents(ctx, w, end)
	if err != nil {
		t.Fatal(err)
	}
	fromEvents := usage.SummarizePricedEvents(events, w, loc, SessionClientName, ver, end)
	if fromEvents.Totals != sum.Totals || fromEvents.EstimatedUSD.String() != sum.EstimatedUSD.String() || fromEvents.PricingVersion != sum.PricingVersion {
		t.Fatalf("ScanEvents summary mismatch: got %+v, want %+v", fromEvents, sum)
	}
}

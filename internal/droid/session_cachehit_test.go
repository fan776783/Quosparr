package droid

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/licoy/quosparr/internal/usage"
)

func TestDroidScanCacheMissingInvalidAndCanceled(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	w := usage.Window{Start: now.Add(-24 * time.Hour), End: now}

	missing := SessionScanner{Dir: filepath.Join(t.TempDir(), "missing")}
	got, err := missing.ScanCache(context.Background(), w, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Totals.Samples != 0 || got.Totals.InputTokens != 0 || len(got.Sessions) != 0 {
		t.Fatalf("missing = %+v, want empty report", got)
	}

	if _, err := missing.ScanCache(context.Background(), usage.Window{Start: now, End: now}, now); !errors.Is(err, errSessionCacheWindow) {
		t.Fatalf("invalid window err = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := missing.ScanCache(ctx, w, now); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled err = %v", err)
	}
}

func TestDroidScanCacheCountsBoundariesAndUnread(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	start := now.Add(-7 * 24 * time.Hour)
	end := now
	w := usage.Window{Start: start, End: end}
	dir := filepath.Join(t.TempDir(), ".factory", "sessions")

	// 1. Exactly at Start (inclusive): input = 100 + 800 + 100 = 1000, cached = 800.
	writeDroidSession(t, filepath.Join(dir, "proj-a", "sess-1.settings.json"), `{
		"model": "claude-opus-5-5",
		"assistantActiveTimeMs": 2000,
		"tokenUsage": {"inputTokens": 100, "cacheReadTokens": 800, "cacheCreationTokens": 100, "outputTokens": 50}
	}`, start)
	writeDroidSession(t, filepath.Join(dir, "proj-a", "sess-1.jsonl"),
		`{"type":"session_start","title":"Alpha Session","cwd":"/tmp/proj-a"}`+"\n", start)

	// 2. custom: model included, no jsonl title -> Name falls back to ID.
	writeDroidSession(t, filepath.Join(dir, "proj-b", "sess-custom.settings.json"), `{
		"model": "custom:local-model",
		"assistantActiveTimeMs": 1000,
		"tokenUsage": {"inputTokens": 200, "cacheReadTokens": 300, "cacheCreationTokens": 0, "outputTokens": 40}
	}`, now.Add(-time.Hour))

	// 3. Exactly at End (exclusive) -> excluded.
	writeDroidSession(t, filepath.Join(dir, "proj-b", "sess-end.settings.json"), `{
		"model": "claude-opus-5-5",
		"tokenUsage": {"inputTokens": 500, "cacheReadTokens": 500}
	}`, end)

	// 4. Before Start -> excluded.
	writeDroidSession(t, filepath.Join(dir, "proj-b", "sess-old.settings.json"), `{
		"model": "claude-opus-5-5",
		"tokenUsage": {"inputTokens": 500, "cacheReadTokens": 500}
	}`, start.Add(-time.Second))

	// 5. Zero input -> skipped.
	writeDroidSession(t, filepath.Join(dir, "proj-b", "sess-zero.settings.json"), `{
		"model": "claude-opus-5-5",
		"tokenUsage": {"inputTokens": 0, "cacheReadTokens": 0, "cacheCreationTokens": 0, "outputTokens": 10}
	}`, now.Add(-30*time.Minute))

	// 6. Malformed settings.json inside window -> UnreadLines++.
	writeDroidSession(t, filepath.Join(dir, "proj-b", "sess-bad.settings.json"), `{bad-json`, now.Add(-15*time.Minute))

	scanner := SessionScanner{Dir: dir}
	report, err := scanner.ScanCache(context.Background(), w, now)
	if err != nil {
		t.Fatal(err)
	}
	if report.UnreadLines != 1 {
		t.Fatalf("UnreadLines = %d, want 1", report.UnreadLines)
	}
	if report.Totals.Samples != 2 || report.Totals.InputTokens != 1500 || report.Totals.CachedInputTokens != 1100 {
		t.Fatalf("totals = %+v", report.Totals)
	}
	if len(report.Models) != 2 || report.Models[0].ID != "claude-opus-5-5" || report.Models[1].ID != "custom:local-model" {
		t.Fatalf("models = %+v", report.Models)
	}
	if len(report.Sessions) != 2 || report.Sessions[0].ID != "sess-1" || report.Sessions[0].Name != "Alpha Session" ||
		report.Sessions[1].ID != "sess-custom" || report.Sessions[1].Name != "sess-custom" {
		t.Fatalf("sessions = %+v", report.Sessions)
	}

	speedSessions, err := scanner.ScanSpeed(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	speedIDs := map[string]bool{}
	for _, s := range speedSessions {
		speedIDs[s.ID] = true
	}
	for _, row := range report.Sessions {
		if !speedIDs[row.ID] {
			t.Fatalf("cache session ID %q does not match ScanSpeed IDs %v", row.ID, speedIDs)
		}
	}
}

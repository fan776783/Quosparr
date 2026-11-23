package droid

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/licoy/quosparr/internal/speed"
)

func TestDroidScanSpeedMissingAndCanceled(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	missing := SessionScanner{Dir: filepath.Join(t.TempDir(), "missing")}
	got, err := missing.ScanSpeed(context.Background(), now)
	if err != nil || got != nil {
		t.Fatalf("missing = (%+v, %v), want (nil, nil)", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := missing.ScanSpeed(ctx, now); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled err = %v", err)
	}
}

func TestDroidScanSpeedMappingFallbacksAndLimits(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	dir := filepath.Join(t.TempDir(), ".factory", "sessions")

	// 1. Complete session with jsonl session_start title/cwd and mtime bumped by jsonl.
	s1Settings := filepath.Join(dir, "dir-alpha", "sess-1.settings.json")
	writeDroidSession(t, s1Settings, `{
		"model": "claude-opus-5-5",
		"reasoningEffort": "high",
		"assistantActiveTimeMs": 4000,
		"tokenUsage": {"inputTokens": 500, "outputTokens": 200}
	}`, now.Add(-10*24*time.Hour))
	writeDroidSession(t, filepath.Join(dir, "dir-alpha", "sess-1.jsonl"),
		`{"type":"session_start","title":"Fix speed scanner","cwd":"/Users/dev/work/quosparr"}`+"\n"+
			`{"type":"message","text":"ignored"}`+"\n",
		now.Add(-time.Hour))

	// 2. custom: model is included in speed, and missing jsonl falls back to project dir name + empty title.
	writeDroidSession(t, filepath.Join(dir, "fallback-proj", "sess-custom.settings.json"), `{
		"model": "custom:local-qwen",
		"reasoningEffort": "medium",
		"assistantActiveTimeMs": 2000,
		"tokenUsage": {"outputTokens": 120}
	}`, now.Add(-30*time.Minute))

	// 3. Missing assistantActiveTimeMs -> skipped.
	writeDroidSession(t, filepath.Join(dir, "proj-skip", "no-active.settings.json"), `{
		"model": "claude-opus-5-5",
		"tokenUsage": {"outputTokens": 100}
	}`, now.Add(-15*time.Minute))

	// 4. Zero outputTokens -> skipped.
	writeDroidSession(t, filepath.Join(dir, "proj-skip", "zero-out.settings.json"), `{
		"model": "claude-opus-5-5",
		"assistantActiveTimeMs": 1500,
		"tokenUsage": {"outputTokens": 0}
	}`, now.Add(-10*time.Minute))

	// 5. Out-of-window -> skipped.
	writeDroidSession(t, filepath.Join(dir, "proj-skip", "too-old.settings.json"), `{
		"model": "claude-opus-5-5",
		"assistantActiveTimeMs": 1500,
		"tokenUsage": {"outputTokens": 100}
	}`, now.Add(-speed.Window-time.Minute))

	// 6. Add 10 older in-window sessions to verify SessionLimit (10) cap.
	for i := 1; i <= 10; i++ {
		p := filepath.Join(dir, "batch", fmt.Sprintf("old-%02d.settings.json", i))
		writeDroidSession(t, p, `{
			"model": "kimi-k3",
			"assistantActiveTimeMs": 1000,
			"tokenUsage": {"outputTokens": 50}
		}`, now.Add(-time.Duration(i+2)*time.Hour))
	}

	got, err := (SessionScanner{Dir: dir}).ScanSpeed(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != speed.SessionLimit {
		t.Fatalf("len(sessions) = %d, want %d", len(got), speed.SessionLimit)
	}

	// Newest first: sess-custom (-30m), then sess-1 (-1h).
	first := got[0]
	if first.ID != "sess-custom" || first.Title != "" || first.ProjectName != "fallback-proj" {
		t.Fatalf("first = %+v", first)
	}
	if len(first.Samples) != 1 || first.Samples[0].Model != "custom:local-qwen" || first.Samples[0].Effort != "medium" ||
		first.Samples[0].HasTTFT || first.Samples[0].OutputTokens != 120 || first.Samples[0].Decode != 2*time.Second {
		t.Fatalf("first sample = %+v", first.Samples)
	}

	second := got[1]
	if second.ID != "sess-1" || second.Title != "Fix speed scanner" || second.ProjectName != "quosparr" {
		t.Fatalf("second = %+v", second)
	}
	if len(second.Samples) != 1 || second.Samples[0].Model != "claude-opus-5-5" || second.Samples[0].Effort != "high" ||
		second.Samples[0].HasTTFT || second.Samples[0].OutputTokens != 200 || second.Samples[0].Decode != 4*time.Second {
		t.Fatalf("second sample = %+v", second.Samples)
	}
}

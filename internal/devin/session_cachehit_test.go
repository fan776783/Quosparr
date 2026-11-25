package devin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
	"github.com/licoy/quosparr/internal/sqliteruntime"
	"github.com/licoy/quosparr/internal/usage"
)

func TestDevinScanCacheMissingInvalidAndCanceled(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	w := usage.Window{Start: now.Add(-24 * time.Hour), End: now}

	missing := SessionScanner{DataDir: t.TempDir()}
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

func TestDevinScanCacheDedupNullsBoundariesAndSessionIDs(t *testing.T) {
	if err := sqliteruntime.Load(); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	start := now.Add(-7 * 24 * time.Hour)
	end := now
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
		`CREATE TABLE sessions (id TEXT PRIMARY KEY, working_directory TEXT, title TEXT, last_activity_at INTEGER)`,
		`CREATE TABLE message_nodes (session_id TEXT, node_id TEXT, chat_message TEXT, created_at INTEGER)`,
		`INSERT INTO sessions(id, working_directory, title, last_activity_at) VALUES ('s1', '/tmp/proj-a', 'Devin Alpha', 0)`,
		`INSERT INTO sessions(id, working_directory, title, last_activity_at) VALUES ('s2', '/tmp/proj-b', 'Devin Beta', 0)`,
	} {
		if _, err := db.Execute(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	insertNode := func(sessionID, nodeID, msg string, createdAt int64) {
		if _, err := db.Execute(ctx, `INSERT INTO message_nodes(session_id, node_id, chat_message, created_at) VALUES (?, ?, ?, ?)`, sessionID, nodeID, msg, createdAt); err != nil {
			t.Fatal(err)
		}
	}

	// 1. At Start (inclusive), complete counters, duplicated twice by message_id.
	//    input = 100 + 800 + 100 = 1000, cached = 800.
	m1 := fmt.Sprintf(`{"message_id":"msg-1","role":"assistant","metadata":{"generation_model":"claude-opus-5-5","started_generation_at":%q,"metrics":{"input_tokens":100,"cache_read_tokens":800,"cache_creation_tokens":100,"total_time_ms":2000,"output_tokens":50}}}`,
		start.Format(time.RFC3339Nano))
	insertNode("s1", "n1", m1, start.Unix())
	insertNode("s1", "n2", m1, start.Unix())

	// 2. Incomplete counters (cache_read_tokens: null), deduped by session_id + node_id -> MissingSamples=1.
	t2 := now.Add(-time.Hour)
	m2 := fmt.Sprintf(`{"role":"assistant","metadata":{"generation_model":"claude-opus-5-5","created_at":%q,"metrics":{"input_tokens":50,"cache_read_tokens":null,"cache_creation_tokens":0,"total_time_ms":1000,"output_tokens":20}}}`,
		t2.Format(time.RFC3339Nano))
	insertNode("s1", "n3", m2, t2.Unix())
	insertNode("s1", "n3", m2, t2.Unix())

	// 3. s2 complete sample.
	t3 := now.Add(-30 * time.Minute)
	m3 := fmt.Sprintf(`{"role":"assistant","metadata":{"message_id":"msg-3","generation_model":"gpt-5","started_generation_at":%q,"metrics":{"input_tokens":200,"cache_read_tokens":200,"cache_creation_tokens":0,"total_time_ms":1500,"output_tokens":40}}}`,
		t3.Format(time.RFC3339Nano))
	insertNode("s2", "n1", m3, t3.Unix())

	// 4. At End (exclusive) -> excluded.
	mEnd := fmt.Sprintf(`{"role":"assistant","metadata":{"message_id":"msg-end","generation_model":"gpt-5","started_generation_at":%q,"metrics":{"input_tokens":500,"cache_read_tokens":500,"cache_creation_tokens":0}}}`,
		end.Format(time.RFC3339Nano))
	insertNode("s2", "n2", mEnd, end.Unix())

	// 5. Before Start -> excluded.
	mOld := fmt.Sprintf(`{"role":"assistant","metadata":{"message_id":"msg-old","generation_model":"gpt-5","started_generation_at":%q,"metrics":{"input_tokens":500,"cache_read_tokens":500,"cache_creation_tokens":0}}}`,
		start.Add(-time.Second).Format(time.RFC3339Nano))
	insertNode("s2", "n3", mOld, start.Add(-time.Second).Unix())

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	scanner := SessionScanner{DataDir: dataDir}
	report, err := scanner.ScanCache(ctx, w, now)
	if err != nil {
		t.Fatal(err)
	}
	if report.Totals.Samples != 2 || report.Totals.MissingSamples != 1 || report.Totals.InputTokens != 1400 || report.Totals.CachedInputTokens != 1000 {
		t.Fatalf("totals = %+v", report.Totals)
	}
	if len(report.Models) != 2 || report.Models[0].ID != "claude-opus-5-5" || report.Models[0].Totals.MissingSamples != 1 ||
		report.Models[1].ID != "gpt-5" || report.Models[1].Totals.MissingSamples != 0 {
		t.Fatalf("models = %+v", report.Models)
	}
	if len(report.Sessions) != 2 || report.Sessions[0].ID != "s1" || report.Sessions[0].Name != "Devin Alpha" ||
		report.Sessions[0].Totals.MissingSamples != 1 || report.Sessions[1].ID != "s2" || report.Sessions[1].Name != "Devin Beta" {
		t.Fatalf("sessions = %+v", report.Sessions)
	}

	speedSessions, err := scanner.ScanSpeed(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	speedIDs := map[string]bool{}
	for _, s := range speedSessions {
		speedIDs[s.ID] = true
	}
	for _, row := range report.Sessions {
		if !speedIDs[row.ID] {
			t.Fatalf("cache session ID %q not in ScanSpeed IDs %v", row.ID, speedIDs)
		}
	}
}

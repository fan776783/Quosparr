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
	"github.com/licoy/quosparr/internal/speed"
	"github.com/licoy/quosparr/internal/sqliteruntime"
)

func TestDevinScanSpeedMissingAndCanceled(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	missing := SessionScanner{DataDir: t.TempDir()}
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

func TestDevinScanSpeedDedupTTFTWindowsAndLimit(t *testing.T) {
	if err := sqliteruntime.Load(); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
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
	} {
		if _, err := db.Execute(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	insertSession := func(id, cwd, title string, lastAct int64) {
		if _, err := db.Execute(ctx, `INSERT INTO sessions(id, working_directory, title, last_activity_at) VALUES (?, ?, ?, ?)`, id, cwd, title, lastAct); err != nil {
			t.Fatal(err)
		}
	}
	insertNode := func(sessionID, nodeID, msg string, createdAt int64) {
		if _, err := db.Execute(ctx, `INSERT INTO message_nodes(session_id, node_id, chat_message, created_at) VALUES (?, ?, ?, ?)`, sessionID, nodeID, msg, createdAt); err != nil {
			t.Fatal(err)
		}
	}

	// Session s1: has started_generation_at, duplicate message_id, null ttft, non-null ttft, and fallback node_id dedup.
	s1Act := now.Add(-10 * time.Minute)
	insertSession("s1", "/Users/dev/work/alpha-repo", "Alpha Task", s1Act.Unix())

	// Message 1 (with ttft_ms=500, total_time_ms=2500 -> Decode=2000ms) duplicated twice by message_id.
	msg1Time := now.Add(-2 * time.Hour)
	msg1 := fmt.Sprintf(`{"message_id":"m-1","role":"assistant","metadata":{"generation_model":"claude-opus-5-5","started_generation_at":%q,"created_at":%q,"metrics":{"ttft_ms":500,"total_time_ms":2500,"output_tokens":200}}}`,
		msg1Time.Format(time.RFC3339Nano), msg1Time.Add(3*time.Second).Format(time.RFC3339Nano))
	insertNode("s1", "n1", msg1, msg1Time.Unix())
	insertNode("s1", "n2", msg1, msg1Time.Unix())

	// Message 2 (without message_id -> dedup by session_id+node_id, ttft_ms=null -> HasTTFT=false, Decode=total_time_ms).
	msg2Time := now.Add(-time.Hour)
	msg2 := fmt.Sprintf(`{"role":"assistant","metadata":{"generation_model":"claude-opus-5-5","created_at":%q,"metrics":{"ttft_ms":null,"total_time_ms":3000,"output_tokens":150}}}`,
		msg2Time.Format(time.RFC3339Nano))
	insertNode("s1", "n3", msg2, msg2Time.Unix())
	insertNode("s1", "n3", msg2, msg2Time.Unix())

	// Out-of-window message (> 7 days old) -> dropped.
	oldTime := now.Add(-speed.Window - time.Minute)
	oldMsg := fmt.Sprintf(`{"role":"assistant","metadata":{"message_id":"m-old","generation_model":"claude-opus-5-5","created_at":%q,"metrics":{"ttft_ms":100,"total_time_ms":1000,"output_tokens":99}}}`,
		oldTime.Format(time.RFC3339Nano))
	insertNode("s1", "n-old", oldMsg, oldTime.Unix())

	// Session out-of-window only -> not included.
	insertSession("s-old", "/Users/dev/work/old", "Old Task", oldTime.Unix())
	insertNode("s-old", "n1", oldMsg, oldTime.Unix())

	// Create 11 more recent sessions (s02..s12) to verify SessionLimit (10) and ordering.
	for i := 2; i <= 12; i++ {
		id := fmt.Sprintf("s%02d", i)
		ts := now.Add(-time.Duration(i) * time.Hour)
		insertSession(id, fmt.Sprintf("/Users/dev/proj-%02d", i), fmt.Sprintf("Task %02d", i), 0)
		body := fmt.Sprintf(`{"role":"assistant","metadata":{"message_id":"m-%02d","generation_model":"gpt-5","started_generation_at":%q,"metrics":{"ttft_ms":400,"total_time_ms":1400,"output_tokens":100}}}`,
			i, ts.Format(time.RFC3339Nano))
		insertNode(id, "n1", body, ts.Unix())
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := (SessionScanner{DataDir: dataDir}).ScanSpeed(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != speed.SessionLimit {
		t.Fatalf("len(sessions) = %d, want %d", len(got), speed.SessionLimit)
	}
	first := got[0]
	if first.ID != "s1" || first.Title != "Alpha Task" || first.ProjectName != "alpha-repo" || !first.UpdatedAt.Equal(s1Act) {
		t.Fatalf("first session = %+v, want s1 / Alpha Task / alpha-repo / %v", first, s1Act)
	}
	if len(first.Samples) != 2 {
		t.Fatalf("s1 samples = %d, want 2 after dedup (%+v)", len(first.Samples), first.Samples)
	}
	sA, sB := first.Samples[0], first.Samples[1]
	if !sA.At.Equal(msg1Time) || !sA.HasTTFT || sA.TTFT != 500*time.Millisecond || sA.Decode != 2000*time.Millisecond || sA.OutputTokens != 200 || sA.Effort != "" || sA.Tier != speed.TierUnknown {
		t.Fatalf("sample 0 = %+v", sA)
	}
	if !sB.At.Equal(msg2Time) || sB.HasTTFT || sB.TTFT != 0 || sB.Decode != 3000*time.Millisecond || sB.OutputTokens != 150 {
		t.Fatalf("sample 1 = %+v", sB)
	}
	// Oldest of the 12 in-window sessions (s11, s12) must be trimmed by SessionLimit=10.
	if got[len(got)-1].ID != "s10" {
		t.Fatalf("last session = %s, want s10", got[len(got)-1].ID)
	}
}

func TestDevinScanSpeedLegacySchemaWithoutSessionsColumns(t *testing.T) {
	if err := sqliteruntime.Load(); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
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
	if _, err := db.Execute(ctx, `CREATE TABLE message_nodes (session_id TEXT, chat_message TEXT, created_at INTEGER)`); err != nil {
		t.Fatal(err)
	}
	ts := now.Add(-30 * time.Minute)
	msg := `{"role":"assistant","metadata":{"generation_model":"claude-opus-5-5","metrics":{"ttft_ms":null,"total_time_ms":2000,"output_tokens":100}}}`
	if _, err := db.Execute(ctx, `INSERT INTO message_nodes(session_id, chat_message, created_at) VALUES ('legacy-1', ?, ?)`, msg, ts.Unix()); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := (SessionScanner{DataDir: dataDir}).ScanSpeed(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "legacy-1" || got[0].Title != "" || got[0].ProjectName != "" || !got[0].UpdatedAt.Equal(ts) {
		t.Fatalf("legacy schema result = %+v", got)
	}
	if len(got[0].Samples) != 1 || got[0].Samples[0].Decode != 2*time.Second {
		t.Fatalf("legacy samples = %+v", got[0].Samples)
	}
}

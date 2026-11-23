package devin

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
	"github.com/licoy/quosparr/internal/pricing"
	"github.com/licoy/quosparr/internal/sqliteruntime"
	"github.com/licoy/quosparr/internal/usage"
	"github.com/licoy/quosparr/internal/usage/decimal"
)

// SessionClientName is the ClientRows label for Devin local sessions.
const SessionClientName = "Devin"

const sessionPricingPrefix = "devin-snapshot"

// SessionDBPath is the SQLite session database path inside DataDir.
func SessionDBPath(dataDir string) string {
	if dataDir == "" {
		return ""
	}
	return filepath.Join(dataDir, "cli", "sessions.db")
}

// SessionDBState returns the current size and modification times of the Devin
// session database and its WAL so callers can skip redundant copies.
func SessionDBState(dataDir string) (sqliteruntime.FileState, bool) {
	path := SessionDBPath(dataDir)
	if path == "" {
		return sqliteruntime.FileState{}, false
	}
	st, err := sqliteruntime.StateOf(path)
	if err != nil {
		return sqliteruntime.FileState{}, false
	}
	return st, true
}

// SessionScanner reads local Devin CLI sessions under DataDir and totals
// their API-equivalent cost.
type SessionScanner struct {
	// DataDir is the Devin CLI data directory (from DataDir(home, getenv)).
	DataDir string
	// Location decides local calendar days; nil means time.Local.
	Location *time.Location
	// PricingSnapshot is captured once per scan.
	PricingSnapshot func() pricing.Snapshot
}

type chatMessageDoc struct {
	Role     string `json:"role"`
	Metadata *struct {
		GenerationModel string `json:"generation_model"`
		CreatedAt       string `json:"created_at"`
		Metrics         *struct {
			InputTokens         int64 `json:"input_tokens"`
			OutputTokens        int64 `json:"output_tokens"`
			CacheReadTokens     int64 `json:"cache_read_tokens"`
			CacheCreationTokens int64 `json:"cache_creation_tokens"`
		} `json:"metrics"`
	} `json:"metadata"`
}

// ScanCost totals the API-equivalent cost of Devin CLI assistant messages in
// [w.Start, w.End) from a private copy of <DataDir>/cli/sessions.db. Missing
// databases yield an empty unavailable Summary without error.
func (s SessionScanner) ScanCost(ctx context.Context, w usage.Window, now time.Time) (usage.Summary, error) {
	if now.IsZero() {
		now = time.Now()
	}
	loc := s.Location
	if loc == nil {
		loc = time.Local
	}
	events, pricingVersion, err := s.ScanEvents(ctx, w, now)
	if err != nil {
		return usage.Summary{}, err
	}
	return usage.SummarizePricedEvents(events, w, loc, SessionClientName, pricingVersion, now), nil
}

// ScanEvents reads Devin CLI assistant messages in [w.Start, w.End) from a
// private copy of <DataDir>/cli/sessions.db and returns the priced events
// together with the vendor pricing version stamp. Missing databases yield nil
// events without error.
func (s SessionScanner) ScanEvents(ctx context.Context, w usage.Window, now time.Time) ([]usage.PricedEvent, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	var snap pricing.Snapshot
	if s.PricingSnapshot != nil {
		snap = s.PricingSnapshot()
	}
	pricingVersion := usage.VendorPricingVersion(sessionPricingPrefix, snap)

	dbPath := SessionDBPath(s.DataDir)
	if dbPath == "" {
		return nil, pricingVersion, nil
	}
	if _, err := os.Lstat(dbPath); errors.Is(err, fs.ErrNotExist) {
		return nil, pricingVersion, nil
	} else if err != nil {
		return nil, "", err
	}

	var events []usage.PricedEvent
	_, err := sqliteruntime.WithCopy(ctx, dbPath, sqliteruntime.DefaultMaxCopyBytes, func(db *sqlite.DB, _ string) error {
		tables, err := sqliteTables(ctx, db)
		if err != nil {
			return err
		}
		if !tables["message_nodes"] {
			return nil
		}
		hasWorkingDir := false
		if tables["sessions"] {
			cols, err := sqliteColumns(ctx, db, "sessions")
			if err != nil {
				return err
			}
			hasWorkingDir = cols["working_directory"]
		}
		query := "SELECT m.session_id, m.created_at, '', m.chat_message FROM message_nodes m WHERE m.created_at >= ? AND m.created_at <= ?"
		if hasWorkingDir {
			query = "SELECT m.session_id, m.created_at, COALESCE(s.working_directory, ''), m.chat_message FROM message_nodes m LEFT JOIN sessions s ON s.id = m.session_id WHERE m.created_at >= ? AND m.created_at <= ?"
		}
		startPad := w.Start.Add(-24 * time.Hour).Unix()
		endPad := w.End.Add(24 * time.Hour).Unix()
		rows, err := db.Query(ctx, query, startPad, endPad)
		if err != nil {
			return err
		}
		for _, row := range rows.Values {
			if err := ctx.Err(); err != nil {
				return err
			}
			if len(row) < 4 {
				continue
			}
			rawMsg := asString(row[3])
			if !strings.Contains(rawMsg, `"metrics"`) || !strings.Contains(rawMsg, `"assistant"`) {
				continue
			}
			var doc chatMessageDoc
			if json.Unmarshal([]byte(rawMsg), &doc) != nil {
				continue
			}
			if doc.Role != "assistant" || doc.Metadata == nil || doc.Metadata.Metrics == nil {
				continue
			}
			ts := parseMessageTime(doc.Metadata.CreatedAt, asInt64(row[1]))
			if ts.IsZero() || ts.Before(w.Start) || !ts.Before(w.End) {
				continue
			}
			m := doc.Metadata.Metrics
			input := max(0, m.InputTokens)
			cacheRead := max(0, m.CacheReadTokens)
			cacheWrite := max(0, m.CacheCreationTokens)
			output := max(0, m.OutputTokens)
			uncached := input + cacheWrite
			total := uncached + cacheRead + output
			if total == 0 {
				continue
			}
			model := strings.TrimSpace(doc.Metadata.GenerationModel)
			if model == "" {
				model = "unknown"
			}
			prompt := input + cacheRead + cacheWrite
			var usd *decimal.Decimal
			var reason string
			if pm, found := snap.LookupVendor(model); !found {
				reason = "unknown-model:" + model
			} else if cost, priced := usage.VendorCost(pm, input, cacheRead, cacheWrite, output, prompt); priced {
				usd = &cost
			} else {
				reason = "missing-price-rule:" + model
			}
			events = append(events, usage.PricedEvent{
				Time:    ts,
				Model:   model,
				Project: usage.ProjectDisplayName(asString(row[2])),
				Session: asString(row[0]),
				Totals: usage.Totals{
					TotalTokens:         total,
					UncachedInputTokens: uncached,
					CachedInputTokens:   cacheRead,
					OutputTokens:        output,
					Turns:               1,
				},
				USD:            usd,
				UnpricedReason: reason,
			})
		}
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	return events, pricingVersion, nil
}

func parseMessageTime(metaCreatedAt string, rowCreatedAt int64) time.Time {
	if s := strings.TrimSpace(metaCreatedAt); s != "" {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t
		}
	}
	if rowCreatedAt > 0 {
		return time.Unix(rowCreatedAt, 0).UTC()
	}
	return time.Time{}
}

func sqliteTables(ctx context.Context, db *sqlite.DB) (map[string]bool, error) {
	rows, err := db.Query(ctx, "SELECT name FROM sqlite_master WHERE type = 'table'")
	if err != nil {
		return nil, err
	}
	tables := map[string]bool{}
	for _, row := range rows.Values {
		if name, ok := row[0].(string); ok {
			tables[name] = true
		}
	}
	return tables, nil
}

func sqliteColumns(ctx context.Context, db *sqlite.DB, table string) (map[string]bool, error) {
	rows, err := db.Query(ctx, "SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		return nil, err
	}
	columns := map[string]bool{}
	for _, row := range rows.Values {
		if name, ok := row[0].(string); ok {
			columns[name] = true
		}
	}
	return columns, nil
}

func asInt64(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case float64:
		return int64(x)
	}
	return 0
}

func asString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	}
	return ""
}

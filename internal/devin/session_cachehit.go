package devin

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
	"github.com/licoy/quosparr/internal/cachehit"
	"github.com/licoy/quosparr/internal/sqliteruntime"
	"github.com/licoy/quosparr/internal/usage"
)

var errSessionCacheWindow = errors.New("devin: invalid cache-hit window")

type cacheChatMessageDoc struct {
	MessageID string `json:"message_id"`
	Role      string `json:"role"`
	Metadata  *struct {
		MessageID           string `json:"message_id"`
		RequestID           string `json:"request_id"`
		GenerationModel     string `json:"generation_model"`
		CreatedAt           string `json:"created_at"`
		StartedGenerationAt string `json:"started_generation_at"`
		Metrics             *struct {
			InputTokens         *int64 `json:"input_tokens"`
			CacheReadTokens     *int64 `json:"cache_read_tokens"`
			CacheCreationTokens *int64 `json:"cache_creation_tokens"`
		} `json:"metrics"`
	} `json:"metadata"`
}

// ScanCache summarizes the cache reads of Devin CLI assistant messages in
// [window.Start, window.End) using the same timestamp and deduplication rules
// as ScanSpeed. Missing databases yield an empty Report without error.
func (s SessionScanner) ScanCache(ctx context.Context, window usage.Window, now time.Time) (cachehit.Report, error) {
	if !window.End.After(window.Start) {
		return cachehit.Report{}, errSessionCacheWindow
	}
	if err := ctx.Err(); err != nil {
		return cachehit.Report{}, err
	}
	if now.IsZero() {
		now = time.Now()
	}
	report := cachehit.Report{Start: window.Start, End: window.End, CalculatedAt: now}
	dbPath := SessionDBPath(s.DataDir)
	if dbPath == "" {
		return report, nil
	}
	if _, err := os.Lstat(dbPath); errors.Is(err, fs.ErrNotExist) {
		return report, nil
	} else if err != nil {
		return cachehit.Report{}, err
	}

	models := map[string]cachehit.Totals{}
	sessions := map[string]cachehit.Totals{}
	names := map[string]string{}
	_, err := sqliteruntime.WithCopy(ctx, dbPath, sqliteruntime.DefaultMaxCopyBytes, func(db *sqlite.DB, _ string) error {
		rows, ok, err := queryMessageNodes(ctx, db, window.Start, window.End)
		if err != nil || !ok {
			return err
		}
		seen := map[string]bool{}
		for _, row := range rows.Values {
			if err := ctx.Err(); err != nil {
				return err
			}
			if len(row) < 7 {
				continue
			}
			rawMsg := asString(row[6])
			if !strings.Contains(rawMsg, `"metrics"`) || !strings.Contains(rawMsg, `"assistant"`) {
				continue
			}
			var doc cacheChatMessageDoc
			if json.Unmarshal([]byte(rawMsg), &doc) != nil {
				continue
			}
			if doc.Role != "assistant" || doc.Metadata == nil || doc.Metadata.Metrics == nil {
				continue
			}
			at := parseMessageTime(doc.Metadata.StartedGenerationAt, 0)
			if at.IsZero() {
				at = parseMessageTime(doc.Metadata.CreatedAt, asInt64(row[2]))
			}
			if at.IsZero() || at.Before(window.Start) || !at.Before(window.End) {
				continue
			}
			m := doc.Metadata.Metrics
			missing := m.InputTokens == nil || m.CacheReadTokens == nil || m.CacheCreationTokens == nil
			var input, cached int64
			if !missing {
				uncached := max(0, *m.InputTokens)
				cached = max(0, *m.CacheReadTokens)
				cacheWrite := max(0, *m.CacheCreationTokens)
				input = uncached + cached + cacheWrite
				if input == 0 {
					continue
				}
			}

			sessionID := asString(row[0])
			dedupKey := messageDedupKey(sessionID, asString(row[1]), firstMessageID(doc.MessageID, doc.Metadata.MessageID), doc.Metadata.RequestID)
			if dedupKey != "" {
				if seen[dedupKey] {
					continue
				}
				seen[dedupKey] = true
			}

			model := strings.TrimSpace(doc.Metadata.GenerationModel)
			if model == "" {
				model = "unknown"
			}
			mt, ct := models[model], sessions[sessionID]
			for _, t := range []*cachehit.Totals{&report.Totals, &mt, &ct} {
				if missing {
					if err := t.AddMissing(); err != nil {
						return err
					}
				} else if err := t.AddSample(input, cached); err != nil {
					return err
				}
			}
			models[model], sessions[sessionID] = mt, ct
			if title := strings.TrimSpace(asString(row[4])); title != "" {
				names[sessionID] = title
			}
		}
		return nil
	})
	if err != nil {
		return cachehit.Report{}, err
	}
	report.Models = sessionCacheRows(models, nil)
	report.Sessions = sessionCacheRows(sessions, names)
	return report, nil
}

func sessionCacheRows(values map[string]cachehit.Totals, names map[string]string) []cachehit.Row {
	rows := make([]cachehit.Row, 0, len(values))
	for id, totals := range values {
		name := id
		if names[id] != "" {
			name = names[id]
		}
		rows = append(rows, cachehit.Row{ID: id, Name: name, Totals: totals})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Totals.InputTokens != rows[j].Totals.InputTokens {
			return rows[i].Totals.InputTokens > rows[j].Totals.InputTokens
		}
		return rows[i].ID < rows[j].ID
	})
	return rows
}

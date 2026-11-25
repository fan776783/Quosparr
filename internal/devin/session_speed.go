package devin

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
	"github.com/licoy/quosparr/internal/speed"
	"github.com/licoy/quosparr/internal/sqliteruntime"
	"github.com/licoy/quosparr/internal/usage"
)

type speedChatMessageDoc struct {
	MessageID string `json:"message_id"`
	Role      string `json:"role"`
	Metadata  *struct {
		MessageID           string `json:"message_id"`
		RequestID           string `json:"request_id"`
		GenerationModel     string `json:"generation_model"`
		CreatedAt           string `json:"created_at"`
		StartedGenerationAt string `json:"started_generation_at"`
		Metrics             *struct {
			TTFTMs       *float64 `json:"ttft_ms"`
			TotalTimeMs  float64  `json:"total_time_ms"`
			OutputTokens int64    `json:"output_tokens"`
		} `json:"metrics"`
	} `json:"metadata"`
}

// ScanSpeed reads the model speed samples of the newest speed.SessionLimit
// sessions updated within speed.Window before now, newest first. Missing
// databases yield nil without error.
func (s SessionScanner) ScanSpeed(ctx context.Context, now time.Time) ([]speed.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if now.IsZero() {
		now = time.Now()
	}
	dbPath := SessionDBPath(s.DataDir)
	if dbPath == "" {
		return nil, nil
	}
	if _, err := os.Lstat(dbPath); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	cutoff := now.Add(-speed.Window)
	var sessions []speed.Session
	_, err := sqliteruntime.WithCopy(ctx, dbPath, sqliteruntime.DefaultMaxCopyBytes, func(db *sqlite.DB, _ string) error {
		rows, ok, err := queryMessageNodes(ctx, db, cutoff, now)
		if err != nil || !ok {
			return err
		}

		seen := map[string]bool{}
		bySession := map[string]*speed.Session{}
		var order []string
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
			var doc speedChatMessageDoc
			if json.Unmarshal([]byte(rawMsg), &doc) != nil {
				continue
			}
			if doc.Role != "assistant" || doc.Metadata == nil || doc.Metadata.Metrics == nil {
				continue
			}
			model := strings.TrimSpace(doc.Metadata.GenerationModel)
			if model == "" {
				continue
			}
			at := parseMessageTime(doc.Metadata.StartedGenerationAt, 0)
			if at.IsZero() {
				at = parseMessageTime(doc.Metadata.CreatedAt, asInt64(row[2]))
			}
			if at.IsZero() || at.Before(cutoff) || at.After(now) {
				continue
			}
			sample, ok := devinSpeedSample(at, model, doc.Metadata.Metrics)
			if !ok {
				continue
			}
			sessionID := asString(row[0])
			dedupKey := messageDedupKey(sessionID, asString(row[1]), firstMessageID(doc.MessageID, doc.Metadata.MessageID), doc.Metadata.RequestID)
			if dedupKey != "" {
				if seen[dedupKey] {
					continue
				}
				seen[dedupKey] = true
			}

			sess := bySession[sessionID]
			if sess == nil {
				var projectName string
				if wd := strings.TrimSpace(asString(row[3])); wd != "" {
					projectName = usage.ProjectDisplayName(wd)
				}
				updatedAt := parseSessionActivityTime(row[5])
				if at.After(updatedAt) {
					updatedAt = at
				}
				sess = &speed.Session{
					ID:          sessionID,
					Title:       strings.TrimSpace(asString(row[4])),
					ProjectName: projectName,
					UpdatedAt:   updatedAt,
				}
				bySession[sessionID] = sess
				order = append(order, sessionID)
			} else if at.After(sess.UpdatedAt) {
				sess.UpdatedAt = at
			}
			sess.Samples = append(sess.Samples, sample)
		}

		sessions = make([]speed.Session, 0, len(order))
		for _, id := range order {
			sessions = append(sessions, *bySession[id])
		}
		sort.SliceStable(sessions, func(i, j int) bool {
			if !sessions[i].UpdatedAt.Equal(sessions[j].UpdatedAt) {
				return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
			}
			return sessions[i].ID < sessions[j].ID
		})
		if len(sessions) > speed.SessionLimit {
			sessions = sessions[:speed.SessionLimit]
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return sessions, nil
}

func devinSpeedSample(at time.Time, model string, m *struct {
	TTFTMs       *float64 `json:"ttft_ms"`
	TotalTimeMs  float64  `json:"total_time_ms"`
	OutputTokens int64    `json:"output_tokens"`
}) (speed.Sample, bool) {
	sample := speed.Sample{
		At:    at,
		Model: model,
		Tier:  speed.TierUnknown,
	}
	total := time.Duration(math.Round(m.TotalTimeMs)) * time.Millisecond
	if m.TTFTMs != nil && *m.TTFTMs >= 0 {
		ttft := time.Duration(math.Round(*m.TTFTMs)) * time.Millisecond
		sample.HasTTFT = true
		sample.TTFT = ttft
		decode := total - ttft
		if m.OutputTokens > 0 && decode > 0 {
			sample.OutputTokens = m.OutputTokens
			sample.Decode = decode
		}
	} else if m.OutputTokens > 0 && total > 0 {
		sample.OutputTokens = m.OutputTokens
		sample.Decode = total
	}
	if !sample.HasTTFT && (sample.OutputTokens <= 0 || sample.Decode <= 0) {
		return speed.Sample{}, false
	}
	return sample, true
}

func parseSessionActivityTime(v any) time.Time {
	switch x := v.(type) {
	case int64:
		if x > 1e12 {
			return time.UnixMilli(x).UTC()
		}
		if x > 0 {
			return time.Unix(x, 0).UTC()
		}
	case float64:
		n := int64(x)
		if n > 1e12 {
			return time.UnixMilli(n).UTC()
		}
		if n > 0 {
			return time.Unix(n, 0).UTC()
		}
	case string:
		if s := strings.TrimSpace(x); s != "" {
			if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
				return t
			}
			if n, err := strconv.ParseInt(s, 10, 64); err == nil {
				return parseSessionActivityTime(n)
			}
		}
	case []byte:
		return parseSessionActivityTime(string(x))
	}
	return time.Time{}
}

func queryMessageNodes(ctx context.Context, db *sqlite.DB, start, end time.Time) (sqlite.Rows, bool, error) {
	tables, err := sqliteTables(ctx, db)
	if err != nil {
		return sqlite.Rows{}, false, err
	}
	if !tables["message_nodes"] {
		return sqlite.Rows{}, false, nil
	}
	msgCols, err := sqliteColumns(ctx, db, "message_nodes")
	if err != nil {
		return sqlite.Rows{}, false, err
	}
	nodeExpr := "''"
	if msgCols["node_id"] {
		nodeExpr = "COALESCE(CAST(m.node_id AS TEXT), '')"
	}

	var joinSessions, hasWorkingDir, hasTitle, hasLastActivity bool
	if tables["sessions"] {
		cols, err := sqliteColumns(ctx, db, "sessions")
		if err != nil {
			return sqlite.Rows{}, false, err
		}
		joinSessions = cols["id"]
		hasWorkingDir = cols["working_directory"]
		hasTitle = cols["title"]
		hasLastActivity = cols["last_activity_at"]
	}

	workingDirExpr := "''"
	if joinSessions && hasWorkingDir {
		workingDirExpr = "COALESCE(s.working_directory, '')"
	}
	titleExpr := "''"
	if joinSessions && hasTitle {
		titleExpr = "COALESCE(s.title, '')"
	}
	lastActExpr := "0"
	if joinSessions && hasLastActivity {
		lastActExpr = "s.last_activity_at"
	}
	joinClause := ""
	if joinSessions {
		joinClause = " LEFT JOIN sessions s ON s.id = m.session_id"
	}
	query := "SELECT m.session_id, " + nodeExpr + ", m.created_at, " + workingDirExpr + ", " + titleExpr + ", " + lastActExpr + ", m.chat_message FROM message_nodes m" + joinClause + " WHERE m.created_at >= ? AND m.created_at <= ?"

	startPad := start.Add(-24 * time.Hour).Unix()
	endPad := end.Add(24 * time.Hour).Unix()
	rows, err := db.Query(ctx, query, startPad, endPad)
	if err != nil {
		return sqlite.Rows{}, false, err
	}
	return rows, true, nil
}

func firstMessageID(ids ...string) string {
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			return id
		}
	}
	return ""
}

func messageDedupKey(sessionID, nodeID, messageID, requestID string) string {
	if msgID := strings.TrimSpace(messageID); msgID != "" {
		return "m:" + msgID
	}
	if reqID := strings.TrimSpace(requestID); reqID != "" {
		return "r:" + reqID
	}
	if nodeID != "" {
		return "n:" + sessionID + "\x00" + nodeID
	}
	return ""
}

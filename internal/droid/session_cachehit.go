package droid

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/licoy/quosparr/internal/cachehit"
	"github.com/licoy/quosparr/internal/usage"
)

var errSessionCacheWindow = errors.New("droid: invalid cache-hit window")

// ScanCache summarizes the cache reads of Droid sessions whose effective
// modification time falls inside [window.Start, window.End). Missing
// directories yield an empty Report without error.
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
	if s.Dir == "" {
		return report, nil
	}
	root := filepath.Clean(s.Dir)
	st, err := os.Stat(root)
	if errors.Is(err, fs.ErrNotExist) {
		return report, nil
	}
	if err != nil {
		return cachehit.Report{}, err
	}
	if !st.IsDir() {
		return cachehit.Report{}, fmt.Errorf("droid: sessions root is not a directory")
	}

	models := map[string]cachehit.Totals{}
	sessions := map[string]cachehit.Totals{}
	names := map[string]string{}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if walkErr != nil {
			if errors.Is(walkErr, fs.ErrNotExist) {
				return nil
			}
			return nil
		}
		name := d.Name()
		if path != root && strings.HasPrefix(name, ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() || !strings.HasSuffix(name, sessionSettingsSuffix) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		sessionTime := info.ModTime()
		jsonlPath := strings.TrimSuffix(path, sessionSettingsSuffix) + ".jsonl"
		if jinfo, err := os.Stat(jsonlPath); err == nil && jinfo.ModTime().After(sessionTime) {
			sessionTime = jinfo.ModTime()
		}
		if sessionTime.Before(window.Start) || !sessionTime.Before(window.End) {
			return nil
		}

		doc, ok := readSessionSettings(path)
		if !ok {
			report.UnreadLines++
			return nil
		}
		if doc.TokenUsage == nil {
			return nil
		}
		uncached := max(0, doc.TokenUsage.InputTokens)
		cached := max(0, doc.TokenUsage.CacheReadTokens)
		cacheWrite := max(0, doc.TokenUsage.CacheCreationTokens)
		input := uncached + cached + cacheWrite
		if input == 0 {
			return nil
		}

		model := strings.TrimSpace(doc.Model)
		if model == "" {
			model = "unknown"
		}
		sessionID := strings.TrimSuffix(name, sessionSettingsSuffix)
		title, _ := readSessionStartMeta(jsonlPath)

		mt, ct := models[model], sessions[sessionID]
		for _, t := range []*cachehit.Totals{&report.Totals, &mt, &ct} {
			if err := t.AddSample(input, cached); err != nil {
				return err
			}
		}
		models[model], sessions[sessionID] = mt, ct
		if title != "" {
			names[sessionID] = title
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

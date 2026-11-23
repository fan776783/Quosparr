package droid

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/licoy/quosparr/internal/speed"
	"github.com/licoy/quosparr/internal/usage"
)

type jsonlSessionStart struct {
	Type  string `json:"type"`
	Title string `json:"title"`
	CWD   string `json:"cwd"`
}

// ScanSpeed reads the session-level model speed samples of the newest
// speed.SessionLimit Droid sessions updated within speed.Window before now,
// newest first. Missing directories yield nil without error.
func (s SessionScanner) ScanSpeed(ctx context.Context, now time.Time) ([]speed.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if now.IsZero() {
		now = time.Now()
	}
	if s.Dir == "" {
		return nil, nil
	}
	root := filepath.Clean(s.Dir)
	st, err := os.Stat(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("droid: sessions root is not a directory")
	}

	cutoff := now.Add(-speed.Window)
	var sessions []speed.Session
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if walkErr != nil {
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
		if sessionTime.Before(cutoff) || sessionTime.After(now) {
			return nil
		}

		doc, ok := readSessionSettings(path)
		if !ok || doc.TokenUsage == nil {
			return nil
		}
		model := strings.TrimSpace(doc.Model)
		if model == "" || doc.TokenUsage.OutputTokens <= 0 || doc.AssistantActiveTimeMs <= 0 {
			return nil
		}
		decode := time.Duration(math.Round(doc.AssistantActiveTimeMs)) * time.Millisecond
		if decode <= 0 {
			return nil
		}

		title, project := readSessionStartMeta(jsonlPath)
		if project == "" {
			parentDir := filepath.Dir(path)
			if parentDir != root {
				if base := filepath.Base(parentDir); base != "" && base != "." && base != string(filepath.Separator) {
					project = base
				}
			}
		}

		sessionID := strings.TrimSuffix(name, sessionSettingsSuffix)
		sessions = append(sessions, speed.Session{
			ID:          sessionID,
			Title:       title,
			ProjectName: project,
			UpdatedAt:   sessionTime,
			Samples: []speed.Sample{{
				At:           sessionTime,
				Model:        model,
				Effort:       strings.TrimSpace(doc.ReasoningEffort),
				Tier:         speed.TierUnknown,
				HasTTFT:      false,
				OutputTokens: doc.TokenUsage.OutputTokens,
				Decode:       decode,
			}},
		})
		return nil
	})
	if err != nil {
		return nil, err
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
	return sessions, nil
}

func readSessionStartMeta(jsonlPath string) (title, project string) {
	f, err := os.Open(jsonlPath)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	r := bufio.NewReader(io.LimitReader(f, sessionFileLimit))
	line, err := r.ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", ""
	}
	line = []byte(strings.TrimSpace(string(line)))
	if len(line) == 0 {
		return "", ""
	}
	var head jsonlSessionStart
	if json.Unmarshal(line, &head) != nil || head.Type != "session_start" {
		return "", ""
	}
	title = strings.TrimSpace(head.Title)
	if cwd := strings.TrimSpace(head.CWD); cwd != "" {
		project = usage.ProjectDisplayName(cwd)
	}
	return title, project
}

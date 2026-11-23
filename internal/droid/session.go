package droid

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/licoy/quosparr/internal/pricing"
	"github.com/licoy/quosparr/internal/usage"
	"github.com/licoy/quosparr/internal/usage/decimal"
)

// SessionClientName is the ClientRows label for Droid local sessions.
const SessionClientName = "Droid"

const (
	sessionSettingsSuffix = ".settings.json"
	sessionFileLimit      = 1 << 20
	sessionPricingPrefix  = "droid-snapshot"
)

// SessionScanner reads local Droid session summaries under Dir (normally
// <home>/.factory/sessions) and totals their API-equivalent cost.
type SessionScanner struct {
	// Dir is the Droid sessions directory, normally <home>/.factory/sessions.
	Dir string
	// Location decides local calendar days; nil means time.Local.
	Location *time.Location
	// PricingSnapshot is captured once per scan.
	PricingSnapshot func() pricing.Snapshot
}

type sessionSettingsDoc struct {
	Model      string `json:"model"`
	TokenUsage *struct {
		InputTokens         int64 `json:"inputTokens"`
		OutputTokens        int64 `json:"outputTokens"`
		CacheCreationTokens int64 `json:"cacheCreationTokens"`
		CacheReadTokens     int64 `json:"cacheReadTokens"`
		ThinkingTokens      int64 `json:"thinkingTokens"`
	} `json:"tokenUsage"`
}

// ScanCost totals the API-equivalent cost of Droid sessions whose effective
// modification time falls inside [w.Start, w.End). Missing directories yield an
// empty unavailable Summary without error.
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

// ScanEvents reads Droid session summaries whose effective modification time
// falls inside [w.Start, w.End) and returns the priced events together with the
// vendor pricing version stamp. Missing directories yield nil events without error.
func (s SessionScanner) ScanEvents(ctx context.Context, w usage.Window, now time.Time) ([]usage.PricedEvent, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	var snap pricing.Snapshot
	if s.PricingSnapshot != nil {
		snap = s.PricingSnapshot()
	}
	pricingVersion := usage.VendorPricingVersion(sessionPricingPrefix, snap)

	if s.Dir == "" {
		return nil, pricingVersion, nil
	}
	root := filepath.Clean(s.Dir)
	st, err := os.Stat(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, pricingVersion, nil
	}
	if err != nil {
		return nil, "", err
	}
	if !st.IsDir() {
		return nil, "", fmt.Errorf("droid: sessions root is not a directory")
	}

	var events []usage.PricedEvent
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
		if sessionTime.Before(w.Start) || !sessionTime.Before(w.End) {
			return nil
		}
		doc, ok := readSessionSettings(path)
		if !ok || doc.TokenUsage == nil {
			return nil
		}
		model := strings.TrimSpace(doc.Model)
		if strings.HasPrefix(strings.ToLower(model), "custom:") {
			return nil
		}
		input := max(0, doc.TokenUsage.InputTokens)
		cacheWrite := max(0, doc.TokenUsage.CacheCreationTokens)
		cacheRead := max(0, doc.TokenUsage.CacheReadTokens)
		output := max(0, doc.TokenUsage.OutputTokens)
		uncached := input + cacheWrite
		total := uncached + cacheRead + output
		if total == 0 {
			return nil
		}
		if model == "" {
			model = "unknown"
		}
		project := usage.UnknownProject
		parentDir := filepath.Dir(path)
		if parentDir != root {
			if base := filepath.Base(parentDir); base != "" && base != "." && base != string(filepath.Separator) {
				project = base
			}
		}
		var usd *decimal.Decimal
		var reason string
		if pm, found := snap.LookupVendor(model); !found {
			reason = "unknown-model:" + model
		} else if cost, priced := usage.VendorCost(pm, input, cacheRead, cacheWrite, output, -1); priced {
			usd = &cost
		} else {
			reason = "missing-price-rule:" + model
		}
		events = append(events, usage.PricedEvent{
			Time:    sessionTime,
			Model:   model,
			Project: project,
			Session: path,
			Totals: usage.Totals{
				TotalTokens:         total,
				UncachedInputTokens: uncached,
				CachedInputTokens:   cacheRead,
				OutputTokens:        output,
				Turns:               0,
			},
			USD:            usd,
			UnpricedReason: reason,
		})
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	return events, pricingVersion, nil
}

func readSessionSettings(path string) (sessionSettingsDoc, bool) {
	f, err := os.Open(path)
	if err != nil {
		return sessionSettingsDoc{}, false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, sessionFileLimit+1))
	if err != nil || len(data) > sessionFileLimit {
		return sessionSettingsDoc{}, false
	}
	var doc sessionSettingsDoc
	if json.Unmarshal(data, &doc) != nil {
		return sessionSettingsDoc{}, false
	}
	return doc, true
}

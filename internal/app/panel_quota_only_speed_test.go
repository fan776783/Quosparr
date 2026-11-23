package app

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/licoy/quosparr/internal/cachehit"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/runway"
	"github.com/licoy/quosparr/internal/speed"
	"github.com/licoy/quosparr/internal/store"
	"github.com/licoy/quosparr/internal/usage"
)

func TestQuotaOnlySpeedDetailSessionCacheAndDroidNote(t *testing.T) {
	l := l10n.New(l10n.English)
	r := speedFixture()
	r.NoTTFT = true
	// Add a third session with empty metadata (no ProjectName, zero UpdatedAt) that still has cache data.
	r.Sessions = append(r.Sessions, speed.SessionStat{
		ID:     "s3",
		Title:  "No meta session",
		Groups: r.Sessions[0].Groups,
	})

	snap := quotaOnlyFixture()
	snap.Droid.Speed = r
	snap.Droid.SpeedSessionCache = map[string]cachehit.Totals{
		"s1": {InputTokens: 1000, CachedInputTokens: 920, Samples: 2},
		"s3": {InputTokens: 1000, CachedInputTokens: 850, Samples: 1, MissingSamples: 1},
		// s2 intentionally omitted -> should not show cache rate
	}
	snap.Devin.Availability = runway.QuotaOnlyReady
	snap.Devin.Meters = []runway.Meter{{Title: "Daily", RemainingPercent: 60, UsedPercent: 40, Source: runway.SourceStandard}}
	snap.Devin.Speed = speedFixture()
	snap.Devin.SpeedSessionCache = map[string]cachehit.Totals{
		"s1": {InputTokens: 1000, CachedInputTokens: 750, Samples: 1},
	}

	a, tt := newPanel(t, func(p *store.Preferences) {
		enableQuotaOnly(p)
		p.SelectedProvider = store.ProviderDroid
		p.ShowsSpeedAnalysis = true
	}, snap, PanelActions{})
	a.panelState.detail = store.ModuleSpeedAnalysis
	tt.Frame()

	for _, want := range []string{
		tx(l, xSessionsCacheHitRate),
		"92%",
		"85% *",
		l.Text(l10n.KeyDroidSpeedInfo),
		"No meta session",
	} {
		if !tt.HasText(want) {
			t.Fatalf("Droid detail missing %q; texts %q", want, tt.Texts())
		}
	}
	if hasExact(tt, "P50") {
		t.Fatalf("Droid detail unexpectedly shows P50; texts %q", tt.Texts())
	}

	// Devin speed detail shows cache rate and P50, and omits Droid-specific note.
	aDevin, ttDevin := newPanel(t, func(p *store.Preferences) {
		enableQuotaOnly(p)
		p.SelectedProvider = store.ProviderDevin
		p.ShowsSpeedAnalysis = true
	}, snap, PanelActions{})
	aDevin.panelState.detail = store.ModuleSpeedAnalysis
	ttDevin.Frame()
	if !ttDevin.HasText(tx(l, xSessionsCacheHitRate)) || !hasExact(ttDevin, "75%") || !hasExact(ttDevin, "P50") {
		t.Fatalf("Devin detail missing cache rate or P50; texts %q", ttDevin.Texts())
	}
	if ttDevin.HasText(l.Text(l10n.KeyDroidSpeedInfo)) {
		t.Fatalf("Devin detail unexpectedly shows KeyDroidSpeedInfo; texts %q", ttDevin.Texts())
	}

	// Codex and Gemini speed details remain on upstream speedDetail (forkDetailBody returns false).
	for _, provider := range []store.Provider{store.ProviderCodex, store.ProviderGemini} {
		s := speedSnapshot(speedFixture())
		s.Gemini = runway.GeminiState{Availability: runway.GeminiReady, Speed: speedFixture()}
		appInst, tester := speedPanel(t, func(p *store.Preferences) { p.SelectedProvider = provider }, s, PanelActions{})
		appInst.panelState.detail = store.ModuleSpeedAnalysis
		tester.Frame()
		if !tester.HasText(speedTestTitle) || tester.HasText(tx(l, xSessionsCacheHitRate)) {
			t.Fatalf("%s detail did not use upstream speedDetail; texts %q", provider, tester.Texts())
		}
	}
}

func TestQuotaOnlyBridgeSpeedSessionCache(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".factory")
	writeDroidLogin(t, dir, "ada@example.com", quotaOnlyNow.Add(time.Hour))
	b := droidTestBridge(t, dir, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(droidLimitsBody))
	})
	p := quotaOnlyPrefs()
	p.ShowsCostSummary = false
	p.ShowsSpeedAnalysis = true
	b.setPreferences(p, []string{"en"})

	b.speedScanner = func(context.Context, time.Time) ([]speed.Session, error) {
		return []speed.Session{{
			ID: "s1", Title: "Session 1", UpdatedAt: quotaOnlyNow.Add(-time.Minute),
			Samples: []speed.Sample{{At: quotaOnlyNow.Add(-time.Minute), Model: "claude-opus-5-5", OutputTokens: 100, Decode: 2 * time.Second}},
		}}, nil
	}
	var cacheErr error
	var gotWin usage.Window
	b.cacheScanner = func(_ context.Context, w usage.Window, _ time.Time) (cachehit.Report, error) {
		gotWin = w
		if cacheErr != nil {
			return cachehit.Report{}, cacheErr
		}
		return cachehit.Report{
			Sessions: []cachehit.Row{
				{ID: "s1", Name: "Session 1", Totals: cachehit.Totals{InputTokens: 500, CachedInputTokens: 450, Samples: 1}},
				{ID: "extra", Name: "Extra", Totals: cachehit.Totals{InputTokens: 900, CachedInputTokens: 100, Samples: 1}},
			},
		}, nil
	}

	if err := b.Refresh(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if !gotWin.Start.Equal(quotaOnlyNow.Add(-speed.Window)) || !gotWin.End.Equal(quotaOnlyNow.Add(time.Nanosecond)) {
		t.Fatalf("cache window = %+v", gotWin)
	}
	st := b.State(quotaOnlyNow)
	if len(st.SpeedSessionCache) != 1 || st.SpeedSessionCache["s1"].CachedInputTokens != 450 {
		t.Fatalf("SpeedSessionCache = %+v, want only s1", st.SpeedSessionCache)
	}
	if _, hasExtra := st.SpeedSessionCache["extra"]; hasExtra {
		t.Fatal("SpeedSessionCache retained session outside Speed.Sessions")
	}

	// Language switch preserves SpeedSessionCache across render rebuild.
	zh := p.Clone()
	zh.Language = store.LanguageSimplifiedChinese
	b.setPreferences(zh, nil)
	if got := b.State(quotaOnlyNow).SpeedSessionCache; len(got) != 1 || got["s1"].CachedInputTokens != 450 {
		t.Fatalf("setPreferences lost SpeedSessionCache: %+v", got)
	}

	// ScanCache error leaves Speed intact and clears map without SpeedError.
	cacheErr = errors.New("cache boom")
	if err := b.RefreshLocal(context.Background()); err != nil {
		t.Fatalf("RefreshLocal err = %v, want nil when only cache fails", err)
	}
	afterCacheErr := b.State(quotaOnlyNow)
	if afterCacheErr.Speed == nil || afterCacheErr.SpeedError != "" || afterCacheErr.SpeedSessionCache != nil {
		t.Fatalf("after cache error = %+v", afterCacheErr)
	}

	// Restore cache then turn off ShowsSpeedAnalysis -> clears SpeedSessionCache.
	cacheErr = nil
	if err := b.RefreshLocal(context.Background()); err != nil {
		t.Fatal(err)
	}
	off := zh.Clone()
	off.ShowsSpeedAnalysis = false
	b.setPreferences(off, nil)
	if st := b.State(quotaOnlyNow); st.Speed != nil || st.SpeedSessionCache != nil {
		t.Fatalf("turning off speed did not clear cache: %+v", st)
	}
}

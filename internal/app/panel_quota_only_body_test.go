package app

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/licoy/quosparr/internal/cachehit"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/runway"
	"github.com/licoy/quosparr/internal/store"
	"github.com/licoy/quosparr/internal/usage"
	"github.com/licoy/quosparr/internal/usage/decimal"
)

func quotaOnlyModularSnapshot() runway.Snapshot {
	now := time.Now()
	usd := decimal.MustParse("12.34")
	costSum := &usage.Summary{
		Window:       usage.Window{Start: now.Add(-24 * time.Hour), End: now},
		Totals:       usage.Totals{TotalTokens: 1200, OutputTokens: 300, Turns: 2},
		EstimatedUSD: &usd,
		CalculatedAt: now,
	}
	snap := quotaOnlyFixture()
	snap.Droid.Meters[0].ResetsAt = now.Add(2 * time.Hour)
	snap.Droid.Meters[0].UsedPercent = 25
	snap.Droid.WindowCosts = []runway.QuotaOnlyWindowCost{
		{Source: runway.SourceStandard, WindowMinutes: runway.FiveHourMinutes, USD: decimal.MustParse("6.00"), Priced: true},
	}
	r := speedFixture()
	r.NoTTFT = true
	snap.Droid.Speed = r
	snap.Droid.Cost = costSum
	snap.Devin = runway.QuotaOnlyState{
		Availability: runway.QuotaOnlyReady,
		Name:         "Ada",
		Meters:       []runway.Meter{{Title: "Daily", WindowMinutes: runway.DailyMinutes, RemainingPercent: 60, UsedPercent: 40, Source: runway.SourceStandard}},
		Speed:        speedFixture(),
		Cost:         costSum,
	}
	return snap
}

func TestPanelQuotaOnlyBodyModuleOrderAndToggles(t *testing.T) {
	l := l10n.New(l10n.English)
	snap := quotaOnlyModularSnapshot()

	for _, provider := range []store.Provider{store.ProviderDroid, store.ProviderDevin} {
		t.Run(string(provider)+"/order_and_disclosures", func(t *testing.T) {
			a, tt := newPanel(t, func(p *store.Preferences) {
				enableQuotaOnly(p)
				p.SelectedProvider = provider
				p.ShowsQuotaSummary = true
				p.ShowsSpeedAnalysis = true
				p.ShowsCacheAnalysis = true
				p.ShowsCostSummary = true
				p.ShowsTokenUsageHeatmap = true
				p.ShowsRecentSessions = true
				p.MainPanelModuleOrder = []store.Module{
					store.ModuleAPICost,
					store.ModuleCacheAnalysis,
					store.ModuleSpeedAnalysis,
					store.ModuleQuota,
				}
			}, snap, PanelActions{})
			a.panelState.cache.summary.report = &cachehit.Report{
				Totals: cachehit.Totals{InputTokens: 1000, CachedInputTokens: 900, Samples: 2},
			}
			tt.Frame()

			quotaAnchor := "Standard · 5-hour"
			if provider == store.ProviderDevin {
				quotaAnchor = "Daily"
			}
			costRect, ok1 := tt.Find(l.Text(l10n.KeyApiCost))
			cacheRect, ok2 := tt.Find(tx(l, xCacheTitle))
			speedRect, ok3 := tt.Find(tx(l, xSpeedTitle))
			quotaRect, ok4 := tt.Find(quotaAnchor)
			if !ok1 || !ok2 || !ok3 || !ok4 {
				t.Fatalf("missing sections (%v %v %v %v); texts %q", ok1, ok2, ok3, ok4, tt.Texts())
			}
			if !(costRect.Y < cacheRect.Y && cacheRect.Y < speedRect.Y && speedRect.Y < quotaRect.Y) {
				t.Fatalf("unexpected module order: cost=%+v cache=%+v speed=%+v quota=%+v", costRect, cacheRect, speedRect, quotaRect)
			}
			if tt.HasText(l.Text(l10n.KeyTokenUsageHeatmap)) || tt.HasText(l.Text(l10n.KeyRecentSessions)) {
				t.Fatalf("unexpected non-fork modules rendered; texts %q", tt.Texts())
			}
			if !tt.HasText("$12.34") {
				t.Fatalf("CostText missing; texts %q", tt.Texts())
			}

			// Clicking "View cache details" opens ModuleCacheAnalysis.
			if err := tt.Click(tx(l, xCacheViewDetails)); err != nil {
				t.Fatal(err)
			}
			if a.panelState.detail != store.ModuleCacheAnalysis {
				t.Fatalf("detail = %q, want %q", a.panelState.detail, store.ModuleCacheAnalysis)
			}

			// Hide speed and cache, then click API cost "Show details".
			a2, tt2 := newPanel(t, func(p *store.Preferences) {
				enableQuotaOnly(p)
				p.SelectedProvider = provider
				p.ShowsQuotaSummary = true
				p.ShowsSpeedAnalysis = false
				p.ShowsCacheAnalysis = false
				p.ShowsCostSummary = true
			}, snap, PanelActions{})
			if tt2.HasText(tx(l, xSpeedTitle)) || tt2.HasText(tx(l, xCacheTitle)) {
				t.Fatalf("disabled speed/cache modules still rendered; texts %q", tt2.Texts())
			}
			if err := tt2.Click(l.Text(l10n.KeyShowDetails)); err != nil {
				t.Fatal(err)
			}
			if a2.panelState.detail != store.ModuleAPICost {
				t.Fatalf("detail = %q, want %q", a2.panelState.detail, store.ModuleAPICost)
			}
		})

		t.Run(string(provider)+"/monitoring_off_shows_opt_in_only", func(t *testing.T) {
			_, tt := newPanel(t, func(p *store.Preferences) {
				p.SelectedProvider = provider
				p.DroidMonitoringEnabled = false
				p.DevinMonitoringEnabled = false
				p.ShowsSpeedAnalysis = true
				p.ShowsCacheAnalysis = true
				p.ShowsCostSummary = true
			}, snap, PanelActions{})
			if !tt.HasText(l.Text(quotaOnlyEnableKey(provider))) {
				t.Fatalf("opt-in button missing; texts %q", tt.Texts())
			}
			for _, hidden := range []string{tx(l, xSpeedTitle), tx(l, xCacheTitle), l.Text(l10n.KeyApiCost)} {
				if tt.HasText(hidden) {
					t.Fatalf("%q shown while monitoring is off; texts %q", hidden, tt.Texts())
				}
			}
		})
	}

	// forkProviderView populates CostText / CostSubtitle / CostLines for Droid.
	snap.Provider = store.ProviderDroid
	v, ok := forkProviderView(l, snap, store.CostRangeToday)
	if !ok || v.CostText == "" || v.DisplayedCost == nil || v.Cost == nil {
		t.Fatalf("forkProviderView(Droid) = (%+v, %v)", v, ok)
	}
}

func TestForkRefreshEverythingAndSyncsCacheSummary(t *testing.T) {
	for _, tc := range []struct {
		p    store.Provider
		want bool
	}{
		{store.ProviderCodex, true},
		{store.ProviderGemini, true},
		{store.ProviderDroid, true},
		{store.ProviderDevin, true},
		{store.ProviderCopilot, false},
		{store.ProviderCursor, false},
	} {
		if got := forkSyncsCacheSummary(tc.p); got != tc.want {
			t.Errorf("forkSyncsCacheSummary(%s) = %v, want %v", tc.p, got, tc.want)
		}
	}

	var sections []runway.Section
	var cacheQueries atomic.Int32
	acts := PanelActions{
		RefreshSection: func(sec runway.Section) { sections = append(sections, sec) },
		QueryCache: func(context.Context, store.Provider, CostQuery) (cachehit.Report, error) {
			cacheQueries.Add(1)
			return cachehit.Report{}, nil
		},
	}
	a, _ := newPanel(t, func(p *store.Preferences) {
		enableQuotaOnly(p)
		p.SelectedProvider = store.ProviderDroid
		p.ShowsCacheAnalysis = true
	}, quotaOnlyModularSnapshot(), acts)
	if !forkRefreshEverything(a, acts) {
		t.Fatal("forkRefreshEverything returned false for Droid")
	}
	if len(sections) != 2 || sections[0] != runway.SectionQuota || sections[1] != runway.SectionAPICost {
		t.Fatalf("Droid refreshed sections = %v, want [quota apiCost]", sections)
	}
	waitSpeedCond(t, "Droid cache summary query", func() bool { return cacheQueries.Load() > 0 })
}

func TestPanelQuotaOnlyCacheTodayFallback(t *testing.T) {
	l := l10n.New(l10n.English)
	todayStr := time.Now().In(time.Local).Format("2006-01-02")

	// 1. Today InputTokens == 0 -> queries Current, shows fallback content + fallback copy, leaves cache.summary untouched.
	t.Run("today_empty_falls_back_to_current", func(t *testing.T) {
		queue := detailMainQueue(t)
		var mu sync.Mutex
		var queries []store.CostRange
		acts := PanelActions{
			QueryCache: func(_ context.Context, _ store.Provider, q CostQuery) (cachehit.Report, error) {
				mu.Lock()
				queries = append(queries, q.Range)
				mu.Unlock()
				if q.Range == store.CostRangeCurrent {
					return cachehit.Report{
						Start:  time.Now().AddDate(0, 0, -6),
						End:    time.Now(),
						Totals: cachehit.Totals{InputTokens: 2000, CachedInputTokens: 1800, Samples: 3},
					}, nil
				}
				return cachehit.Report{Totals: cachehit.Totals{}}, nil
			},
		}
		a, tt := newPanel(t, func(p *store.Preferences) {
			enableQuotaOnly(p)
			p.SelectedProvider = store.ProviderDroid
			p.ShowsQuotaSummary = false
			p.ShowsSpeedAnalysis = false
			p.ShowsCostSummary = false
			p.ShowsCacheAnalysis = true
		}, quotaOnlyModularSnapshot(), acts)
		todayReport := cachehit.Report{Start: time.Now(), End: time.Now(), Totals: cachehit.Totals{}}
		a.panelState.cache.summary = cacheQueryFixture(a, todayReport, CostQuery{Range: store.CostRangeToday})
		a.panelState.cache.summary.day = todayStr
		tt.Frame()
		finishDetail(t, queue)
		tt.Frame()

		mu.Lock()
		gotQueries := append([]store.CostRange(nil), queries...)
		mu.Unlock()
		if len(gotQueries) != 1 || gotQueries[0] != store.CostRangeCurrent {
			t.Fatalf("queries = %v, want [current]", gotQueries)
		}
		if !tt.HasText("90%") || !tt.HasText(l.Text(l10n.KeyQuotaOnlyCycleFallback)) {
			t.Fatalf("fallback rate or text missing; texts %q", tt.Texts())
		}
		if a.panelState.cache.summary.report == nil || a.panelState.cache.summary.report.Totals.InputTokens != 0 {
			t.Fatalf("cache.summary was overwritten: %+v", a.panelState.cache.summary.report)
		}

		// Header refresh clears forkCache and re-queries Today.
		if !forkRefreshEverything(a, acts) {
			t.Fatal("forkRefreshEverything returned false")
		}
		if a.panelState.forkCache.report != nil || a.panelState.forkCache.query != nil {
			t.Fatalf("forkRefreshEverything did not clear forkCache: %+v", a.panelState.forkCache)
		}
		finishDetail(t, queue)
	})

	// 2. Today has InputTokens > 0 -> does not issue Current fallback query.
	t.Run("today_non_empty_skips_fallback", func(t *testing.T) {
		var mu sync.Mutex
		var queries []store.CostRange
		acts := PanelActions{
			QueryCache: func(_ context.Context, _ store.Provider, q CostQuery) (cachehit.Report, error) {
				mu.Lock()
				queries = append(queries, q.Range)
				mu.Unlock()
				return cachehit.Report{}, nil
			},
		}
		a, tt := newPanel(t, func(p *store.Preferences) {
			enableQuotaOnly(p)
			p.SelectedProvider = store.ProviderDevin
			p.ShowsQuotaSummary = false
			p.ShowsSpeedAnalysis = false
			p.ShowsCostSummary = false
			p.ShowsCacheAnalysis = true
		}, quotaOnlyModularSnapshot(), acts)
		todayReport := cachehit.Report{Totals: cachehit.Totals{InputTokens: 500, CachedInputTokens: 400, Samples: 1}}
		a.panelState.cache.summary = cacheQueryFixture(a, todayReport, CostQuery{Range: store.CostRangeToday})
		a.panelState.cache.summary.day = todayStr
		tt.Frame()
		mu.Lock()
		gotQueries := append([]store.CostRange(nil), queries...)
		mu.Unlock()
		if len(gotQueries) != 0 {
			t.Fatalf("unexpected fallback queries when today has data: %v", gotQueries)
		}
		if !tt.HasText("80%") || tt.HasText(l.Text(l10n.KeyQuotaOnlyCycleFallback)) {
			t.Fatalf("unexpected fallback display; texts %q", tt.Texts())
		}
	})

	// 3. Fallback Current also empty -> displays Today empty summary without fallback notice.
	t.Run("fallback_also_empty_shows_today_empty", func(t *testing.T) {
		queue := detailMainQueue(t)
		acts := PanelActions{
			QueryCache: func(_ context.Context, _ store.Provider, _ CostQuery) (cachehit.Report, error) {
				return cachehit.Report{Totals: cachehit.Totals{}}, nil
			},
		}
		a, tt := newPanel(t, func(p *store.Preferences) {
			enableQuotaOnly(p)
			p.SelectedProvider = store.ProviderDroid
			p.ShowsQuotaSummary = false
			p.ShowsSpeedAnalysis = false
			p.ShowsCostSummary = false
			p.ShowsCacheAnalysis = true
		}, quotaOnlyModularSnapshot(), acts)
		todayReport := cachehit.Report{Totals: cachehit.Totals{}}
		a.panelState.cache.summary = cacheQueryFixture(a, todayReport, CostQuery{Range: store.CostRangeToday})
		a.panelState.cache.summary.day = todayStr
		tt.Frame()
		finishDetail(t, queue)
		tt.Frame()
		if tt.HasText(l.Text(l10n.KeyQuotaOnlyCycleFallback)) {
			t.Fatalf("fallback notice shown when Current is also empty; texts %q", tt.Texts())
		}
		if !tt.HasText("—") {
			t.Fatalf("today empty summary dash missing; texts %q", tt.Texts())
		}
	})
}

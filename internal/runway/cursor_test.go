package runway

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/licoy/quosparr/internal/export"
	"github.com/licoy/quosparr/internal/store"
)

type fakeCursor struct{ count atomic.Int32 }

func (f *fakeCursor) State(time.Time) CursorState         { return CursorState{Availability: CursorReady} }
func (f *fakeCursor) SetOnChange(func())                  {}
func (f *fakeCursor) Refresh(context.Context, bool) error { f.count.Add(1); return nil }

func TestCursorRefreshRequiresOptInAndVisibleSurface(t *testing.T) {
	c := &fakeCursor{}
	s, _ := newService(t, newFake(t), func(o *Options) { o.Cursor = c })
	refreshAll(t, s)
	<-s.RefreshCursor(true)
	if c.count.Load() != 0 {
		t.Fatal("disabled Cursor refreshed")
	}
	p := s.Preferences()
	p.CursorMonitoringEnabled = true
	s.SetPreferences(p)
	settle(t, s)
	if c.count.Load() != 1 {
		t.Fatal("enabling visible provider did not refresh")
	}
	p.PanelProviders = []store.Provider{store.ProviderCodex}
	s.SetPreferences(p)
	settle(t, s)
	s.SetWidgetRequirements(export.RequireProviderQuota)
	refreshAll(t, s)
	if c.count.Load() != 1 {
		t.Fatal("unrelated widget refreshed hidden Cursor")
	}
	<-s.RefreshCursor(false)
	if c.count.Load() != 1 {
		t.Fatal("background refresh fetched hidden Cursor")
	}
	// Cursor can now sit beyond the legacy first pair in a status component.
	p.SetStatusBarGroups([][]store.Provider{{store.ProviderCodex, store.ProviderClaude}, {store.ProviderCursor}})
	s.SetPreferences(p)
	settle(t, s)
	if c.count.Load() != 2 {
		t.Fatal("Cursor in a later status component did not refresh")
	}
	<-s.RefreshCursor(true)
	if c.count.Load() != 3 {
		t.Fatal("explicit refresh from account settings was ignored")
	}
}

func TestCursorPanelDoesNotReadCodexLocalData(t *testing.T) {
	s, f, g, c, reports := localService(t)
	p := s.Preferences()
	p.SelectedProvider = store.ProviderCursor
	s.SetPreferences(p)
	s.SetPanelOpen(true)
	settle(t, s)
	if got := localCounts(f, reports); got != [4]int{} || g.localCount() != 0 || c.localCount() != 0 {
		t.Fatal("Cursor opening scanned another provider")
	}
}

func TestCursorStatusAndFloatingKeepPoolNamesAndHideStaleMeters(t *testing.T) {
	p := store.DefaultPreferences()
	p.SelectedProvider = store.ProviderCursor
	in := StatusInputs{Prefs: p, Provider: store.ProviderCursor, Cursor: CursorState{Availability: CursorReady, Meters: []Meter{{Title: "Auto pool", RemainingPercent: 75, UsedPercent: 25, PreserveTitle: true}}}}
	v := BuildStatusBar(zh, in)
	if len(v.Components) != 1 || v.Components[0].Provider != store.ProviderCursor || v.Components[0].Cells[0].Percent != "75%" {
		t.Fatal("Cursor status fell through")
	}
	f := BuildFloating(zh, in)
	if len(f.Providers) != 1 || f.Providers[0].Meters[0].Label != "Auto pool" {
		t.Fatal("pool name was replaced with a window")
	}
	in.Cursor.Stale = true
	v = BuildStatusBar(zh, in)
	if !v.Components[0].Cells[0].Empty || v.Components[0].Cells[0].Percent != "--" || len(BuildFloating(zh, in).Providers[0].Meters) != 0 {
		t.Fatal("stale quota advertised as live")
	}
}

func TestCursorStatusBarPoolsSurviveGroupingAndWindowSelection(t *testing.T) {
	p := store.DefaultPreferences()
	p.SetStatusBarQuotaWindow(store.StatusBarWindowAuto)
	p.SetStatusBarGroups([][]store.Provider{{store.ProviderCodex, store.ProviderClaude}, {store.ProviderCursor}, {store.ProviderGrok}})
	in := StatusInputs{Prefs: p, Cursor: CursorState{Availability: CursorReady, Meters: []Meter{
		{Title: "Cursor models", PreserveTitle: true, RemainingPercent: 75},
		{Title: "Other models", PreserveTitle: true, RemainingPercent: 40},
	}}}
	for _, style := range store.StatusBarStyles() {
		in.Prefs.StatusBarDisplayStyle = style
		v := BuildStatusBar(zh, in)
		found := false
		for _, comp := range v.Components {
			if comp.Provider != store.ProviderCursor {
				continue
			}
			found = true
			if len(comp.Cells) != 2 || comp.Cells[0].Label != "Cursor models" || comp.Cells[1].Label != "Other models" || comp.Cells[1].Percent != "40%" {
				t.Fatalf("%s Cursor cells = %+v", style, comp.Cells)
			}
		}
		if !found || !strings.Contains(v.Tooltip, "Other models 40%") {
			t.Fatalf("%s lost Cursor: %+v", style, v)
		}
	}
	// A chosen time window falls back to Cursor's first pool; the pool is
	// never renamed to a window that the service did not report.
	in.Prefs.StatusBar.Window = store.StatusBarWindowFiveHour
	in.Prefs.StatusBarDisplayStyle = store.StatusBarStyleMeters
	in.Prefs.SetStatusBarGroups([][]store.Provider{{store.ProviderCodex, store.ProviderCursor}})
	v := BuildStatusBar(zh, in)
	if len(v.Components) != 1 || len(v.Components[0].Cells) != 2 || v.Components[0].Cells[1].Provider != store.ProviderCursor || v.Components[0].Cells[1].Percent != "75%" {
		t.Fatalf("stacked Cursor = %+v", v)
	}
	in.Cursor.Stale = true
	v = BuildStatusBar(zh, in)
	if !v.Components[0].Cells[1].Empty || strings.Contains(v.Tooltip, "75%") {
		t.Fatalf("stale Cursor advertised as live: %+v", v)
	}
}

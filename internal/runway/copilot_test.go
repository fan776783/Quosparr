package runway

import (
	"context"
	"github.com/licoy/quosparr/internal/export"
	"github.com/licoy/quosparr/internal/store"
	"sync/atomic"
	"testing"
	"time"
)

type fakeCopilot struct{ count atomic.Int32 }

func (f *fakeCopilot) State(time.Time) CopilotState        { return CopilotState{Availability: CopilotReady} }
func (f *fakeCopilot) Refresh(context.Context, bool) error { f.count.Add(1); return nil }
func (*fakeCopilot) SetOnChange(func())                    {}
func TestCopilotRefreshIsolationAndDisplayState(t *testing.T) {
	c := &fakeCopilot{}
	s, _ := newService(t, newFake(t), func(o *Options) { o.Copilot = c })
	<-s.RefreshCopilot(true)
	if c.count.Load() != 0 {
		t.Fatal("disabled monitoring refreshed")
	}
	p := s.Preferences()
	p.CopilotMonitoringEnabled = true
	s.SetPreferences(p)
	settle(t, s)
	if c.count.Load() != 1 {
		t.Fatal("enable did not refresh")
	}
	p.PanelProviders = []store.Provider{store.ProviderCodex}
	s.SetPreferences(p)
	settle(t, s)
	s.SetWidgetRequirements(export.RequireProviderQuota)
	refreshAll(t, s)
	if c.count.Load() != 1 {
		t.Fatal("unrelated widget fetched hidden Copilot")
	}
	p.SelectedProvider = store.ProviderCopilot
	p.SetStatusBarGroups([][]store.Provider{{store.ProviderCopilot}})
	in := StatusInputs{Prefs: p, Provider: store.ProviderCopilot, Copilot: CopilotState{Availability: CopilotReady, Meters: []Meter{{Title: "AI Credits", PreserveTitle: true, RemainingPercent: 89}}}}
	if b := BuildStatusBar(zh, in); len(b.Components) != 1 || b.Components[0].Cells[0].Percent != "89%" {
		t.Fatal("status bar used another provider")
	}
	if f := BuildFloating(zh, in); len(f.Providers) != 1 || len(f.Providers[0].Meters) != 1 {
		t.Fatal("floating quota missing")
	}
	in.Copilot.Stale = true
	if b := BuildStatusBar(zh, in); !b.Components[0].Cells[0].Empty || len(BuildFloating(zh, in).Providers[0].Meters) != 0 {
		t.Fatal("stale quota remained live")
	}
}

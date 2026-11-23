package app

import (
	"time"

	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/pricing"
	"github.com/licoy/quosparr/internal/runway"
	"github.com/licoy/quosparr/internal/store"
)

// forkRuntime owns the quota-only bridges. Keep their lifecycle and UI actions
// here so upstream provider additions do not need parallel wiring blocks.
type forkRuntime struct {
	copilotB *copilotBridge
	cursorB  *cursorBridge
	droidB   *quotaOnlyBridge
	devinB   *quotaOnlyBridge
}

func newForkRuntime(home, version string, prefs store.Preferences, langs []string) forkRuntime {
	return forkRuntime{
		copilotB: newCopilotBridge(home, prefs, langs),
		cursorB:  newCursorBridge(home, prefs, langs),
		droidB:   newDroidBridge(home, prefs, langs),
		devinB:   newDevinBridge(home, version, prefs, langs),
	}
}

func (f forkRuntime) providers() runway.ForkProviders {
	return runway.ForkProviders{Copilot: f.copilotB, Cursor: f.cursorB, Droid: f.droidB, Devin: f.devinB}
}

func (f forkRuntime) setPricingSnapshot(fn func() pricing.Snapshot) {
	for _, b := range []*quotaOnlyBridge{f.droidB, f.devinB} {
		if b != nil {
			b.pricingSnapshot = fn
		}
	}
}

func (rt *liveApp) quotaOnlyPricesChanged() {
	for _, item := range []struct {
		p store.Provider
		b *quotaOnlyBridge
	}{{store.ProviderDroid, rt.droidB}, {store.ProviderDevin, rt.devinB}} {
		if item.b == nil {
			continue
		}
		item.b.invalidateCost()
		if rt.svc != nil {
			rt.svc.RefreshQuotaOnlyLocal(item.p)
		}
	}
}

func (rt *liveApp) closeForkProviders() {
	if rt.copilotB != nil {
		rt.copilotB.close()
	}
	if rt.cursorB != nil {
		rt.cursorB.close()
	}
}

func (rt *liveApp) setForkPreferences(p store.Preferences) {
	if rt.copilotB != nil {
		rt.copilotB.setPreferences(p, rt.langs)
	}
	if rt.cursorB != nil {
		rt.cursorB.setPreferences(p, rt.langs)
	}
	for _, b := range []*quotaOnlyBridge{rt.droidB, rt.devinB} {
		if b != nil {
			b.setPreferences(p, rt.langs)
		}
	}
}

func (rt *liveApp) addForkPanelActions(actions *PanelActions, s *Settings) {
	actions.AuthorizeCopilot = func() { rt.svc.AuthorizeCopilot() }
	actions.SetCopilotMonitoring = func(on bool) {
		if s != nil {
			s.SetCopilotMonitoringEnabled(on)
		}
	}
	actions.AuthorizeCursor = func() { rt.requestCursorAuthorization(s) }
	actions.SetQuotaOnlyMonitoring = func(p store.Provider, on bool) {
		if s != nil {
			s.SetQuotaOnlyMonitoring(p, on)
		}
	}
	actions.SetCursorMonitoring = func(on bool) {
		if s != nil {
			s.SetCursorMonitoringEnabled(on)
		}
	}
}

func refreshForkSection(s *runway.Service, sec runway.Section) bool {
	switch s.Preferences().SelectedProvider {
	case store.ProviderCopilot:
		if sec == runway.SectionQuota {
			s.RefreshCopilot(true)
		}
	case store.ProviderCursor:
		if sec == runway.SectionQuota {
			s.RefreshCursor(true)
		}
	case store.ProviderDroid, store.ProviderDevin:
		switch sec {
		case runway.SectionQuota:
			s.RefreshQuotaOnly(s.Preferences().SelectedProvider, true)
		case runway.SectionAPICost:
			s.RefreshQuotaOnlyLocal(s.Preferences().SelectedProvider)
		}
	default:
		return false
	}
	return true
}

func forkStatusBarSample(l l10n.Localizer, now time.Time) runway.ForkSnapshot {
	return runway.ForkSnapshot{
		Cursor: runway.CursorState{Availability: runway.CursorReady, Meters: []runway.Meter{
			{Title: l.Text(l10n.KeyCursorAuto), PreserveTitle: true, RemainingPercent: 73, Health: runway.HealthGreen, ResetsAt: now.Add(400 * time.Hour)},
			{Title: l.Text(l10n.KeyCursorAPI), PreserveTitle: true, RemainingPercent: 41, Health: runway.HealthYellow, ResetsAt: now.Add(400 * time.Hour)}}},
	}
}

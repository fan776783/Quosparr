package app

import (
	"github.com/egoist/mygo/ui"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/runway"
	"github.com/licoy/quosparr/internal/store"
)

// The panel header dispatch of the fork's providers, consulted before the
// upstream branches; false for an upstream provider.

// forkIdentityRow draws the read-only identity row of a fork provider.
func forkIdentityRow(c *ui.Context, l l10n.Localizer, prefs store.Preferences, snap runway.Snapshot) bool {
	switch p := prefs.SelectedProvider; p {
	case store.ProviderCopilot:
		copilotIdentityRow(c, l, prefs, snap.Copilot)
	case store.ProviderCursor:
		cursorIdentityRow(c, l, prefs, snap.Cursor)
	case store.ProviderDroid, store.ProviderDevin:
		quotaOnlyIdentityRow(c, l, prefs, quotaOnlyState(snap, p), p)
	default:
		return false
	}
	return true
}

func forkPanelBusy(snap runway.Snapshot) (busy, ok bool) {
	switch snap.Provider {
	case store.ProviderCopilot:
		return snap.Copilot.Refreshing, true
	case store.ProviderCursor:
		return snap.Cursor.Refreshing, true
	case store.ProviderDroid, store.ProviderDevin:
		return quotaOnlyState(snap, snap.Provider).Refreshing, true
	}
	return false, false
}

func forkProviderMeters(snap runway.Snapshot, p store.Provider) ([]runway.Meter, bool) {
	switch p {
	case store.ProviderCopilot:
		return snap.Copilot.LiveMeters(), true
	case store.ProviderCursor:
		return snap.Cursor.LiveMeters(), true
	case store.ProviderDroid, store.ProviderDevin:
		return quotaOnlyState(snap, p).Meters, true
	}
	return nil, false
}

// compactProviderTabs is the most provider tabs that show every name.
const compactProviderTabs = 4

// compactProviderTab names a provider tab in its tooltip and, beyond
// compactProviderTabs tabs, narrows it to its logo, the selected one keeping
// its name on a wider tab; it reports whether the tab is narrowed.
func compactProviderTab(tab ui.Element, name string, count int, selected bool) bool {
	tab.Tooltip(name)
	if count <= compactProviderTabs {
		return false
	}
	tab.Padding(6, 4, 7, 4)
	if selected {
		tab.Grow(2)
	}
	return true
}

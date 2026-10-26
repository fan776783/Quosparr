package app

import (
	"github.com/egoist/mygo/ui"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/store"
)

// Cursor is appended after the upstream account kinds, never inserted into them.
func cursorAccountKind() int { return len(store.Providers()) }

func renderCursorAccountsPage(c *ui.Context, u *settingsUI, f *accountForm, rt *liveApp) bool {
	if f.kind != cursorAccountKind() {
		return false
	}
	renderCursorAccount(c, u, rt.svc.Snapshot().Cursor, func() { rt.svc.RefreshCursor(true) }, func() { rt.requestCursorAuthorization(u.L()) })
	return true
}

func cursorAccountTabs(c *ui.Context, l l10n.Localizer, selected *int, busy bool) bool {
	providers := store.DisplayProviders()
	return providerTabsOf(c, providers, selected, busy, providerLabels(l, providers)...)
}

package app

import (
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/store"
	"testing"
)

func TestCursorStatusBarPreviewHasPoolsAndIcon(t *testing.T) {
	l := l10n.New(l10n.English)
	p := store.DefaultPreferences()
	p.SetStatusBarQuotaWindow(store.StatusBarWindowAuto)
	p.SelectedProvider = store.ProviderCursor
	p.StatusBar.RingValues = true
	if trayProviderIcon(store.ProviderCursor) == nil {
		t.Fatal("Cursor tray icon missing")
	}
	for _, style := range store.StatusBarStyles() {
		p.StatusBarDisplayStyle = style
		r := statusBarSample(l, p)
		if len(r.Components) != 1 || r.Components[0].Provider != store.ProviderCursor || len(r.Components[0].Cells) != 2 {
			t.Fatalf("%s preview = %+v", style, r)
		}
		cells := r.Components[0].Cells
		if cells[0].Empty || cells[0].Label != l.Text(l10n.KeyCursorAuto) || cells[1].Label != l.Text(l10n.KeyCursorAPI) {
			t.Fatalf("%s preview pools = %+v", style, cells)
		}
		if style == store.StatusBarStyleRings && (!r.RingValues || cells[0].RingText != "73" || cells[1].RingText != "41" || !ringValuesFit(cells)) {
			t.Fatalf("Cursor ring values missing: %+v", r)
		}
	}
}

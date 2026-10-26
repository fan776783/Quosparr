package app

import (
	"github.com/egoist/mygo/ui"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/store"
	"slices"
	"testing"
)

func TestCursorIsDisplayOnlyAndNotAnUpstreamAccountProvider(t *testing.T) {
	base := store.Providers()
	if len(base) == 0 || slices.Contains(base, store.ProviderCursor) {
		t.Fatal("Cursor leaked into upstream provider catalog")
	}
	if !slices.Equal(store.DisplayProviders(), append(slices.Clone(base), store.ProviderCursor)) {
		t.Fatal("Cursor display provider missing")
	}
	e := newEnv(t, nil)
	u := newSettingsUI(e.s, SettingsOptions{Pricing: func() PricingActions {
		return PricingActions{Snapshot: func() PricingSnapshot { return PricingSnapshot{} }}
	}})
	u.tab = tabModelPricing
	tt := ui.NewTester(u.view, settingsWidth, settingsHeight)
	for _, key := range []l10n.Key{l10n.KeyProviderCodex, l10n.KeyProviderClaude, l10n.KeyProviderGrok} {
		if !hasExact(tt, u.L().Text(key)) {
			t.Fatal("pricing provider tab missing")
		}
	}
	if hasExact(tt, "Cursor") {
		t.Fatal("Cursor leaked into model pricing management")
	}
}

func TestCursorDisplaySelectionControlsKeepAllProviders(t *testing.T) {
	base := store.Providers()
	e := newEnv(t, nil)
	if err := e.s.Load(); err != nil {
		t.Fatal(err)
	}
	e.s.SetPanelProviders(store.DisplayProviders())
	u := newSettingsUI(e.s, SettingsOptions{})
	tt := ui.NewTester(func(c *ui.Context) {
		settingsGroup(c, "", "", nil, func(row rowFunc) { u.panelProviderRowsFor(c, row, e.s.Prefs(), false, store.DisplayProviders()) })
	}, settingsWidth, settingsHeight)
	if !hasExact(tt, "Cursor") {
		t.Fatal("Cursor custom display choice missing")
	}
	if err := tt.Click("Cursor"); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(e.s.Prefs().DisplayPanelSelection(), store.ProviderCursor) {
		t.Fatal("Cursor display selection did not change")
	}
	if !slices.Equal(store.Providers(), base) {
		t.Fatal("display action mutated base catalog")
	}
}

package app

import (
	"github.com/egoist/mygo/ui"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/runway"
	"github.com/licoy/quosparr/internal/store"
	"testing"
)

func TestCopilotPanelQuotaOnlyAndAllTabsFit(t *testing.T) {
	for _, lang := range []store.Language{store.LanguageEnglish, store.LanguageSimplifiedChinese, store.LanguageTraditionalChinese, store.LanguageKorean, store.LanguageJapanese, store.LanguageRussian, store.LanguageFrench} {
		enabled, refreshes := false, 0
		a, tt := newPanel(t, func(p *store.Preferences) { p.SelectedProvider = store.ProviderCopilot; p.Language = lang }, panelFixture(), PanelActions{SetCopilotMonitoring: func(v bool) { enabled = v }, Refresh: func() { t.Error("Copilot refreshed other provider modules") }, RefreshSection: func(s runway.Section) {
			if s != runway.SectionQuota {
				t.Error("non-quota section refreshed")
			}
			refreshes++
		}})
		if tt.HasText("Ada Lovelace") || tt.HasText(a.L().Text(l10n.KeySessionRepair)) {
			t.Fatal("Copilot showed Codex content")
		}
		if e := tt.Click(a.L().Copilot("enable")); e != nil || !enabled {
			t.Fatal("explicit enable action failed")
		}
		refreshEverything(a, a.PanelActions())
		if refreshes != 1 {
			t.Fatal("quota refresh not dispatched")
		}
		for _, p := range store.DisplayProviders() {
			r, ok := tt.Find(a.L().Text(providerKey(p)))
			if !ok || r.X < 0 || r.X+r.W > panelWidth {
				t.Fatalf("provider tab outside panel for %s", lang)
			}
		}
	}
}

func TestCopilotSourceChoicePersistsAndClearsAccount(t *testing.T) {
	u := cardSettingsUI(t, store.LanguageEnglish)
	u.s.SetCopilotMonitoringEnabled(true)
	u.s.SetCopilotAccount("previous-account")
	tt := ui.NewTester(func(c *ui.Context) { copilotSourceControl(c, u.s, false) }, 500, 300)
	if e := tt.Click(u.L().Copilot("source")); e != nil {
		t.Fatal(e)
	}
	if e := tt.Click("VS Code"); e != nil {
		t.Fatal(e)
	}
	p := u.s.Prefs()
	if p.CopilotSource != "vscode" || p.CopilotAccountID != "" {
		t.Fatal("source selection failed or retained another source's account")
	}
}

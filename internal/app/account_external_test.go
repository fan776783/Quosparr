package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/store"
)

func TestExternalAccountCardShowsQuotaLikeAccountRows(t *testing.T) {
	u := cardSettingsUI(t, store.LanguageEnglish)
	l := u.L()
	q := quotaOnlyFixture().Droid
	e := externalAccount{name: q.Name, email: q.Email, plan: q.Plan, meters: q.Meters, details: q.Details, updated: q.UpdatedAt}
	refreshes, empties := 0, 0
	const width = settingsMinWidth - settingsSidebarWidth
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Padding(16).Gap(14).Children(func() {
			externalAccountList(c, u, store.ProviderDroid, e, false, func() { refreshes++ }, func() { empties++ })
		})
	}, width, 640)
	for _, text := range []string{q.Name, q.Email, q.Plan, accountCountTitle(l, 1), l.Text(l10n.KeyAccountsCurrent), q.Meters[0].Title, q.Meters[1].Title, l10n.Format("%d%% %s", 25, l.Text(l10n.KeyLeft)), q.Details[0].Title} {
		r, ok := tt.Find(text)
		if !ok || r.X < -0.5 || r.X+r.W > width+0.5 {
			t.Fatalf("card text %q missing or outside the page: %+v", text, r)
		}
	}
	if empties != 0 {
		t.Fatal("signed-in login drew the empty state")
	}
	for _, key := range []l10n.Key{l10n.KeyAccountsActions, l10n.KeyAccountsMakeCurrent, l10n.KeyAccountsAdd} {
		if tt.HasText(l.Text(key)) {
			t.Fatal("read-only login offered account actions")
		}
	}
	if err := tt.Click(l.Text(l10n.KeyRefresh)); err != nil || refreshes != 1 {
		t.Fatal("card refresh missing")
	}

	e.warning = "quota read failed"
	tt.Frame()
	if !tt.HasText(e.warning) || !tt.HasText(q.Meters[0].Title) {
		t.Fatal("a failed read hid the last quota or its failure")
	}

	u.s.SetAccountPrivacyMode(true)
	tt.Frame()
	if tt.HasText(q.Email) || !tt.HasText(maskAccount(true, q.Email)) {
		t.Fatal("card ignored privacy mode")
	}

	e = externalAccount{empty: "Sign in"}
	tt.Frame()
	if empties == 0 || !tt.HasText(accountCountTitle(l, 0)) || tt.HasText(l.Text(l10n.KeyAccountsCurrent)) {
		t.Fatal("signed-out provider drew a card")
	}
	if err := tt.Click(l.Text(l10n.KeyRefresh)); err != nil || refreshes != 2 {
		t.Fatal("signed-out provider lost its refresh")
	}
}

package app

import (
	"testing"

	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/runway"
	"github.com/licoy/quosparr/internal/store"
)

func quotaOnlyFixture() runway.Snapshot {
	snap := panelFixture()
	snap.Droid = runway.QuotaOnlyState{
		Availability: runway.QuotaOnlyReady, Name: "Ada Lovelace", Email: "ada@example.com", Plan: "Team",
		Meters: []runway.Meter{
			{Title: "Standard · 5-hour", WindowMinutes: runway.FiveHourMinutes, RemainingPercent: 77, UsedPercent: 23, Source: runway.SourceStandard},
			{Title: "Core · Weekly", WindowMinutes: runway.WeeklyMinutes, RemainingPercent: 25, UsedPercent: 75, Source: runway.SourceModelSpecific},
		},
		Details: []runway.DetailLine{{Title: "Plan", Value: "Team"}},
	}
	snap.Devin = runway.QuotaOnlyState{Availability: runway.QuotaOnlyNotInstalled, LastError: l10n.New(l10n.English).Text(l10n.KeyDevinNotInstalled)}
	return snap
}

func enableQuotaOnly(p *store.Preferences) {
	p.DroidMonitoringEnabled, p.DevinMonitoringEnabled = true, true
}

func TestPanelQuotaOnlyShowsOnlyItsQuota(t *testing.T) {
	l := l10n.New(l10n.English)
	a, tt := newPanel(t, func(p *store.Preferences) { enableQuotaOnly(p); p.SelectedProvider = store.ProviderDroid }, quotaOnlyFixture(), PanelActions{})
	for _, want := range []string{"Standard · 5-hour", "Core · Weekly", "Ada Lovelace", "Team"} {
		if !tt.HasText(want) {
			t.Fatalf("%q missing; texts %q", want, tt.Texts())
		}
	}
	for _, hidden := range []string{"quota-headline", l.Text(l10n.KeyRecentSessions), l.Text(l10n.KeyRateLimitResetTodayUnavailableHint)} {
		if tt.HasText(hidden) {
			t.Fatalf("%q shown for Droid", hidden)
		}
	}
	_ = tt.Click("Ada Lovelace")
	if a.panelState.detail != "" {
		t.Fatal("identity opened an account page")
	}
}

func TestPanelQuotaOnlyEmptyStateAndPrivacy(t *testing.T) {
	l := l10n.New(l10n.English)
	_, tt := newPanel(t, func(p *store.Preferences) { enableQuotaOnly(p); p.SelectedProvider = store.ProviderDevin }, quotaOnlyFixture(), PanelActions{})
	for _, want := range []string{l.Text(l10n.KeyDevinNotInstalled), l.Text(l10n.KeyDevinInstallHint), l.Text(l10n.KeyDevinOpenInstallGuide)} {
		if !tt.HasText(want) {
			t.Fatalf("%q missing; texts %q", want, tt.Texts())
		}
	}
	_, private := newPanel(t, func(p *store.Preferences) {
		enableQuotaOnly(p)
		p.SelectedProvider = store.ProviderDroid
		p.AccountPrivacyModeEnabled = true
	}, quotaOnlyFixture(), PanelActions{})
	if private.HasText("Ada Lovelace") {
		t.Fatal("privacy mode shows the name")
	}
}

func TestProviderStripCompactsSixTabs(t *testing.T) {
	_, tt := newPanel(t, func(p *store.Preferences) { enableQuotaOnly(p); p.SelectedProvider = store.ProviderDroid }, quotaOnlyFixture(), PanelActions{})
	l := l10n.New(l10n.English)
	selected, ok := tt.Find(l.Text(l10n.KeyProviderDroid))
	other, found := tt.Find(l.Text(l10n.KeyProviderGemini))
	if !ok || !found {
		t.Fatal("provider tabs lost their accessible names")
	}
	if other.W >= selected.W || other.X+other.W > panelWidth {
		t.Fatalf("tabs do not compact: selected %v, other %v", selected, other)
	}
}

func TestPanelQuotaOnlyOffersOptIn(t *testing.T) {
	l := l10n.New(l10n.English)
	var got []bool
	acts := PanelActions{SetQuotaOnlyMonitoring: func(p store.Provider, on bool) {
		if p == store.ProviderDevin {
			got = append(got, on)
		}
	}}
	_, tt := newPanel(t, func(p *store.Preferences) { p.SelectedProvider = store.ProviderDevin }, quotaOnlyFixture(), acts)
	if !tt.HasText(l.Text(l10n.KeyDevinHint)) || tt.HasText(l.Text(l10n.KeyDevinNotInstalled)) {
		t.Fatalf("disabled Devin panel; texts %q", tt.Texts())
	}
	if err := tt.Click(l.Text(l10n.KeyDevinEnable)); err != nil || len(got) != 1 || !got[0] {
		t.Fatalf("enable click %v %v", err, got)
	}
}

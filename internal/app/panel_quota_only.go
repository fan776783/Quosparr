package app

import (
	"math"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/runway"
	"github.com/licoy/quosparr/internal/store"
	"github.com/licoy/quosparr/internal/usage/decimal"
)

// Install guides of the quota-only providers.
const (
	droidInstallGuideURL = "https://docs.factory.ai/cli/getting-started/overview"
	devinInstallGuideURL = "https://docs.devin.ai/cli"
)

// SetQuotaOnlyMonitoring turns Droid or Devin monitoring on or off.
func (s *Settings) SetQuotaOnlyMonitoring(provider store.Provider, on bool) bool {
	return s.Update(func(p *store.Preferences) {
		switch provider {
		case store.ProviderDroid:
			p.DroidMonitoringEnabled = on
		case store.ProviderDevin:
			p.DevinMonitoringEnabled = on
		}
	})
}

// quotaOnlyState is the state of Droid or Devin in the snapshot.
func quotaOnlyState(snap runway.Snapshot, p store.Provider) runway.QuotaOnlyState {
	if p == store.ProviderDevin {
		return snap.Devin
	}
	return snap.Droid
}

func quotaOnlyEnableKey(p store.Provider) l10n.Key {
	if p == store.ProviderDevin {
		return l10n.KeyDevinEnable
	}
	return l10n.KeyDroidEnable
}

func quotaOnlyHintKey(p store.Provider) l10n.Key {
	if p == store.ProviderDevin {
		return l10n.KeyDevinHint
	}
	return l10n.KeyDroidHint
}

// panelQuotaOnly is the quota section of Droid or Devin: the opt-in switch
// while monitoring is off, else the meters, a warning when the last read
// failed, and the plan and balances behind the info button. Each Droid
// window also shows its own reset time.
func panelQuotaOnly(c *ui.Context, l l10n.Localizer, prefs store.Preferences, snap runway.Snapshot, p store.Provider, acts PanelActions, first *bool, st *panelState) {
	q := quotaOnlyState(snap, p)
	on := prefs.MonitoringEnabled(p)
	spec := sectionSpec{title: l.Text(providerKey(p)), refreshing: q.Refreshing}
	if on {
		spec.refresh = sectionRefresh(acts, runway.SectionQuota)
	}
	if on && len(q.Details) > 0 {
		spec.info = &sectionInfo{id: string(p) + "-quota", help: l.Text(l10n.KeyQuotaOnlyDetails), body: quotaOnlyDetailsBody(c, l, st, q.Details)}
	}
	sectionBlock(c, first, func() {
		drawSectionHead(c, l, st, spec)
		if !on {
			dimText(c, l.Text(quotaOnlyHintKey(p)))
			set := acts.SetQuotaOnlyMonitoring
			if accentButton(c, icPlug, l.Text(quotaOnlyEnableKey(p)), set == nil).Clicked() && set != nil {
				set(p, true)
			}
			return
		}
		if len(q.Meters) == 0 {
			if q.Refreshing && q.Availability == runway.QuotaOnlyLoading {
				sectionEmpty(c, l, true)
			} else {
				quotaOnlyEmptyState(c, l, p, q)
			}
			return
		}
		if q.Availability != runway.QuotaOnlyReady && q.LastError != "" {
			ui.Text(c, q.LastError).FontSize(fsCaption).TextColor(sys(c.Theme(), sysOrange)).MaxLines(2)
		}
		rs := st.resetSwitch(prefs.QuotaResetTimeFormat)
		ui.Column(c).Gap(12).Children(func() {
			now := time.Now()
			for i, m := range q.Meters {
				mc := c.Key(string(p) + "-" + m.Title)
				if p == store.ProviderDroid && i > 0 {
					slimMeterRow(mc, l, m, now, &rs)
					continue
				}
				grokMeterRow(mc, l, m, i == 0, now, rs)
			}
			if prefs.ShowsCostSummary {
				for _, line := range quotaOnlyCostLines(l, q.Meters, q.WindowCosts) {
					quotaLineRow(c, line)
				}
			}
		})
		if prefs.ShowsCostSummary {
			if disclosureRow(c, nil, l.Text(l10n.KeyShowDetails), true).Clicked() {
				st.detail = store.ModuleAPICost
			}
		}
	})
}

// quotaOnlyCostLines formats per-window local API-equivalent costs matching
// Cursor's "used / estimated_total · Estimated" rows.
func quotaOnlyCostLines(l l10n.Localizer, meters []runway.Meter, costs []runway.QuotaOnlyWindowCost) []runway.DetailLine {
	if len(meters) == 0 || len(costs) == 0 {
		return nil
	}
	var lines []runway.DetailLine
	for _, m := range meters {
		if m.Source != runway.SourceStandard || m.WindowMinutes <= 0 || !m.HasReset() {
			continue
		}
		wc, ok := findWindowCost(costs, m.Source, m.WindowMinutes)
		if !ok {
			continue
		}
		if !wc.Priced {
			val := quotaOnlyUSD(decimal.Zero)
			if wc.Partial {
				val = l.Pricing("unpriced")
			}
			lines = append(lines, runway.DetailLine{
				Title: m.Title + " · " + l.Text(l10n.KeyUsed),
				Value: val,
			})
			continue
		}
		if m.UsedPercent >= 1 && wc.USD.Sign() > 0 {
			total := wc.USD.Mul(decimal.FromInt(100)).Div(decimal.FromInt(m.UsedPercent))
			val := quotaOnlyUSD(wc.USD) + " / " + quotaOnlyUSD(total) + " · " + l.Text(l10n.KeyCursorEstimated)
			if wc.Partial {
				val += " · " + l.Pricing("partial")
			}
			lines = append(lines, runway.DetailLine{Title: m.Title, Value: val})
			continue
		}
		val := quotaOnlyUSD(wc.USD)
		if wc.Partial {
			val += " · " + l.Pricing("partial")
		}
		lines = append(lines, runway.DetailLine{
			Title: m.Title + " · " + l.Text(l10n.KeyUsed),
			Value: val,
		})
	}
	return lines
}

func findWindowCost(costs []runway.QuotaOnlyWindowCost, source runway.MeterSource, minutes int) (runway.QuotaOnlyWindowCost, bool) {
	for _, wc := range costs {
		if wc.Source == source && wc.WindowMinutes == minutes {
			return wc, true
		}
	}
	return runway.QuotaOnlyWindowCost{}, false
}

func quotaOnlyUSD(v decimal.Decimal) string {
	f, _ := strconv.ParseFloat(v.String(), 64)
	cents := int64(math.Round(f * 100))
	if cents < 0 {
		cents = 0
	}
	return cursorMoney(cents)
}

// quotaOnlyEmptyState explains why there is no reading and links the guide
// that sets the client up.
func quotaOnlyEmptyState(c *ui.Context, l l10n.Localizer, p store.Provider, q runway.QuotaOnlyState) {
	t := c.Theme()
	title, detail, icon, tint := quotaOnlyEmptyCopy(l, p, q)
	cardColumn(c, surfaceSunkenKind, radiusCard).Gap(7).Padding(12).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon).Size(13, 13).TextColor(tint(t))
			ui.Text(c, title).FontSize(fsCallout).FontWeight(600).TextColor(tint(t))
		})
		if detail != "" {
			dimText(c, detail)
		}
		if q.Availability != runway.QuotaOnlyNotInstalled && q.Availability != runway.QuotaOnlyNotLoggedIn {
			return
		}
		label, url := l.Text(l10n.KeyDroidOpenInstallGuide), droidInstallGuideURL
		if p == store.ProviderDevin {
			label, url = l.Text(l10n.KeyDevinOpenInstallGuide), devinInstallGuideURL
		}
		if linkButton(c, label).Clicked() {
			c.OpenURL(url)
		}
	})
}

func quotaOnlyEmptyCopy(l l10n.Localizer, p store.Provider, q runway.QuotaOnlyState) (title, detail string, icon *ui.SVG, tint func(*ui.Theme) ui.Color) {
	secondary := func(t *ui.Theme) ui.Color { return inkSecondary(t) }
	warn := func(t *ui.Theme) ui.Color { return sys(t, sysOrange) }
	hint := l.Text(l10n.KeyDroidInstallHint)
	if p == store.ProviderDevin {
		hint = l.Text(l10n.KeyDevinInstallHint)
	}
	switch q.Availability {
	case runway.QuotaOnlyLoading:
		return l.Text(l10n.KeyQuotaOnlyRefreshing), "", icRefresh, secondary
	case runway.QuotaOnlyReady:
		return l.Text(l10n.KeyQuotaOnlyNoData), "", icUsers, secondary
	case runway.QuotaOnlyNotInstalled:
		return q.LastError, hint, icTerminal, secondary
	case runway.QuotaOnlyNotLoggedIn:
		return q.LastError, hint, icUsers, secondary
	case runway.QuotaOnlyReauthRequired:
		return q.LastError, "", icKey, warn
	case runway.QuotaOnlyNoSubscription, runway.QuotaOnlyUnsupported:
		return q.LastError, "", icKey, secondary
	}
	title = q.LastError
	if title == "" {
		title = l.Text(l10n.KeyQuotaOnlyRefreshFailed)
	}
	return title, "", icAlert, warn
}

// quotaOnlyDetailsBody is the quota details popover.
func quotaOnlyDetailsBody(c *ui.Context, l l10n.Localizer, st *panelState, lines []runway.DetailLine) func() {
	t := c.Theme()
	return func() {
		ui.Text(c, l.Text(l10n.KeyQuotaOnlyDetails)).FontSize(fsTitle3).FontWeight(600)
		cardColumn(c, surfaceSunkenKind, radiusCard).Children(func() {
			for i, line := range lines {
				tableRowRule(c, i == 0)
				ui.Row(c).Gap(10).Padding(7, 10).Children(func() {
					ui.Text(c, line.Title).FontSize(fsCallout).TextColor(inkSecondary(t)).Grow(1).Basis(0)
					ui.Text(c, line.Value).FontSize(fsCallout).FontFeatures("tnum").TextAlign(ui.End)
				})
			}
		})
		ui.Row(c).Children(func() {
			ui.Spacer(c).Grow(1)
			if PillButton(c, nil, l.Text(l10n.KeyOk), false).Clicked() {
				st.info = ""
			}
		})
	}
}

// quotaOnlyIdentity is the signed-in name, else the email, masked in privacy
// mode; "" without a login.
func quotaOnlyIdentity(prefs store.Preferences, q runway.QuotaOnlyState) string {
	name := q.Name
	if name == "" {
		name = q.Email
	}
	if name == "" {
		return ""
	}
	return accountPrivate(prefs.AccountPrivacyModeEnabled, name)
}

func quotaOnlySignedOutKey(p store.Provider) l10n.Key {
	if p == store.ProviderDevin {
		return l10n.KeyDevinNotLoggedIn
	}
	return l10n.KeyDroidNotLoggedIn
}

// quotaOnlyIdentityRow shows the account and the plan, like Copilot's row:
// there is no account library behind it.
func quotaOnlyIdentityRow(c *ui.Context, l l10n.Localizer, prefs store.Preferences, q runway.QuotaOnlyState, p store.Provider) {
	ui.Row(c).Gap(8).Padding(8, 0, 0, 0).AlignItems(ui.Center).Children(func() {
		name := quotaOnlyIdentity(prefs, q)
		switch {
		case !prefs.MonitoringEnabled(p):
			name = l.Text(providerKey(p))
		case name == "":
			name = l.Text(quotaOnlySignedOutKey(p))
		}
		ui.Text(c, name).FontSize(fsCallout).Grow(1).Basis(0).SingleLine().Ellipsis("…")
		if q.Plan != "" {
			runwayTagPadded(c, q.Plan, toneNeutral, 1, 5)
		}
	})
}

// compactSettingsTabs is the most provider tabs a settings row names at its
// minimum width.
const compactSettingsTabs = 6

// providerTabLabelsSelected names every tab while they fit; beyond that only
// the selected one keeps its name and the others show their logo, whose
// accessible name and tooltip stay the brand name.
func providerTabLabelsSelected(l l10n.Localizer, providers []store.Provider, selected int) []string {
	labels := providerLabels(l, providers)
	if len(providers) <= compactSettingsTabs {
		return labels
	}
	for i := range labels {
		if i != selected {
			labels[i] = ""
		}
	}
	return labels
}

// Droid and Devin follow Copilot in the account tabs.
func quotaOnlyAccountKind(p store.Provider) int {
	if p == store.ProviderDevin {
		return len(store.Providers()) + 3
	}
	return len(store.Providers()) + 2
}

// renderQuotaOnlyAccountsPage is the Droid or Devin account tab: the
// monitoring switch and the current login, read-only.
func renderQuotaOnlyAccountsPage(c *ui.Context, u *settingsUI, f *accountForm, rt *liveApp) bool {
	var p store.Provider
	switch f.kind {
	case quotaOnlyAccountKind(store.ProviderDroid):
		p = store.ProviderDroid
	case quotaOnlyAccountKind(store.ProviderDevin):
		p = store.ProviderDevin
	default:
		return false
	}
	s, prefs, l := u.s, u.s.Prefs(), u.L()
	q := quotaOnlyState(rt.svc.Snapshot(), p)
	settingsGroup(c, l.Text(providerKey(p)), "", nil, func(row rowFunc) {
		row(l.Text(quotaOnlyEnableKey(p)), l.Text(quotaOnlyHintKey(p)), func() {
			if v, ok := toggle(c, string(p)+"-monitor", l.Text(quotaOnlyEnableKey(p)), prefs.MonitoringEnabled(p), s.ReadOnly()); ok {
				s.SetQuotaOnlyMonitoring(p, v)
			}
		})
	})
	if !prefs.MonitoringEnabled(p) {
		return true
	}
	e := externalAccount{
		name: q.Name, email: q.Email, plan: q.Plan, empty: runway.QuotaOnlyStatusText(l, q, time.Now()),
		meters: q.Meters, details: q.Details, updated: q.UpdatedAt,
	}
	if q.Availability != runway.QuotaOnlyReady {
		e.warning = q.LastError
	}
	externalAccountList(c, u, p, e, q.Refreshing, func() { rt.svc.RefreshQuotaOnly(p, true) }, func() {
		if q.Refreshing && q.Availability == runway.QuotaOnlyLoading {
			externalAccountEmpty(c, e)
			return
		}
		quotaOnlyEmptyState(c, l, p, q)
	})
	return true
}

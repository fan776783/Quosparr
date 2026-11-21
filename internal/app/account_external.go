package app

import (
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/runway"
	"github.com/licoy/quosparr/internal/store"
)

// externalAccount is the one login of a provider without an account library
// (Cursor, Copilot, Droid, Devin). Its card looks like a settings account
// row but reads the provider's state and offers no account actions.
type externalAccount struct {
	name, email, plan string
	// warning is the failure of the last read; empty is the status shown
	// when there is nothing else to show.
	warning, empty string
	meters         []runway.Meter
	details        []runway.DetailLine
	updated        time.Time
}

func (e externalAccount) signedIn() bool {
	return e.name != "" || e.email != "" || e.plan != "" || len(e.meters) > 0
}

// externalAccountList is the account list of such a provider: the count and
// the privacy switch, then the login's card, else the empty state with the
// refresh beside the count.
func externalAccountList(c *ui.Context, u *settingsUI, p store.Provider, e externalAccount, refreshing bool, refresh func(), empty func()) {
	l, prefs := u.L(), u.s.Prefs()
	priv := prefs.AccountPrivacyModeEnabled
	signedIn := e.signedIn()
	ui.Row(c).Gap(8).Padding(0, 0, 0, 12).AlignItems(ui.Center).Children(func() {
		ui.Text(c, accountCountTitle(l, pick(signedIn, 1, 0))).FontSize(13).FontWeight(600).Grow(1).Basis(0).SingleLine()
		if refreshing {
			ui.Spinner(c).Size(14, 14)
		}
		if !signedIn && IconButton(c, icRefresh, l.Text(l10n.KeyRefresh), refreshing).Clicked() {
			refresh()
		}
		if privacyButton(c, l, priv, 26, 26, 5).Clicked() {
			u.s.SetAccountPrivacyMode(!priv)
		}
	})
	if !signedIn {
		empty()
		return
	}
	name := e.name
	if name == "" {
		name = e.email
	}
	if name == "" {
		name = l.Text(providerKey(p))
	}
	view := quotaOptionsFor(prefs, true)
	view.wide = true
	accountRows(c, 1, func(int) {
		externalAccountRow(c, l, p, &e, name, priv, view, func() {
			if IconButton(c, icRefresh, l.Text(l10n.KeyRefresh), refreshing).Clicked() {
				refresh()
			}
		})
	})
}

// externalAccountRow is laid out like accountSettingsRow: the logo, the name
// with its tags and the email, the update time and the controls, then the
// quota under the name. It is the fork's own so the upstream row stays as is.
func externalAccountRow(c *ui.Context, l l10n.Localizer, p store.Provider, e *externalAccount, name string, priv bool, view accountQuotaOptions, controls func()) {
	row := ui.Column(c.Key("account-row-"+string(p))).Gap(4).Padding(10, 8).Margin(0, -8).Radius(8)
	wash := ui.Transparent
	if row.Hovered() {
		wash = pal(c.Theme()).Hover
	}
	row.Background(row.AnimateColor("hover", wash, accountRowFade)).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ProviderIcon(c, p, 20)
			ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(4).Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					shown := maskAccount(priv, name)
					ui.Text(c, shown).FontSize(13).FontWeight(600).SingleLine().MinWidth(0).Shrink(1).Tooltip(shown)
					runwayTagPadded(c, l.Text(l10n.KeyAccountsCurrent), toneGreen, 1, 5)
					if e.plan != "" {
						runwayTagPadded(c, e.plan, toneNeutral, 1, 5)
					}
				})
				if e.email != "" && e.email != name {
					email := maskAccount(priv, e.email)
					dimText(c, email).SingleLine().Tooltip(email)
				}
			})
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				accountQuotaUpdated(c, l, e.updated)
				controls()
			})
		})
		ui.Column(c).Gap(8).Padding(0, 0, 0, 30).Children(func() { externalQuotaBlock(c, l, e, view, priv) })
	})
}

// externalQuotaBlock is the quota of such a login: the failure of the last
// read, the meters in the card's grid and the extra figures as tiles.
func externalQuotaBlock(c *ui.Context, l l10n.Localizer, e *externalAccount, view accountQuotaOptions, privacy bool) {
	ui.Column(c).Gap(10).Children(func() {
		switch {
		case e.warning != "":
			diag := accountDiagnosticText(e.warning, privacy)
			ui.Text(c, diag).FontSize(11).TextColor(sys(c.Theme(), sysOrange)).MaxLines(2).Tooltip(diag)
		case len(e.meters) == 0 && len(e.details) == 0:
			dimText(c, e.empty)
		}
		if len(e.meters) > 0 {
			accountMeters(c, view, len(e.meters), func(i int) {
				accountMeter(c, l, projectedAccountMeter(e.meters[i], view.current), view)
			})
		}
		stats := make([]accountStat, 0, len(e.details))
		for _, d := range e.details {
			stats = append(stats, accountStat{label: d.Title, value: d.Value})
		}
		accountStats(c, view, stats)
	})
}

// externalAccountEmpty is the status of a provider without a login.
func externalAccountEmpty(c *ui.Context, e externalAccount) {
	if e.warning != "" {
		ui.Text(c, e.warning).FontSize(12).TextColor(sys(c.Theme(), sysOrange)).MaxLines(2)
		return
	}
	ui.Text(c, e.empty).FontSize(12).TextColor(c.Theme().TextMuted)
}

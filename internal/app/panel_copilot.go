package app

import (
	"github.com/egoist/mygo/ui"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/runway"
	"github.com/licoy/quosparr/internal/store"
	"time"
)

const copilotUsageURL = "https://github.com/settings/copilot/features"

func (s *Settings) SetCopilotMonitoringEnabled(on bool) bool {
	return s.Update(func(p *store.Preferences) { p.CopilotMonitoringEnabled = on })
}
func (s *Settings) SetCopilotSource(source string) bool {
	return s.Update(func(p *store.Preferences) { p.CopilotSource = source; p.CopilotAccountID = "" })
}
func (s *Settings) SetCopilotAccount(id string) bool {
	return s.Update(func(p *store.Preferences) { p.CopilotAccountID = id })
}
func copilotIdentityRow(c *ui.Context, l l10n.Localizer, p store.Preferences, s runway.CopilotState) {
	ui.Row(c).Gap(8).Padding(8, 0, 0, 0).Children(func() {
		name := s.Email
		if name == "" {
			name = l.Text(l10n.KeyProviderCopilot)
		}
		ui.Text(c, accountPrivate(p.AccountPrivacyModeEnabled, name)).FontSize(fsCallout).Grow(1).Basis(0).SingleLine()
		if s.Plan != "" {
			runwayTagPadded(c, s.Plan, toneNeutral, 1, 5)
		}
	})
}
func copilotAuthorizationButton(c *ui.Context, l l10n.Localizer, s runway.CopilotState, authorize func()) {
	if s.NeedsAuthorization && accentButton(c, icPlug, l.Copilot("authorize"), s.Refreshing || authorize == nil).Clicked() && authorize != nil {
		authorize()
	}
}
func panelCopilotQuota(c *ui.Context, l l10n.Localizer, p store.Preferences, s runway.CopilotState, acts PanelActions, first *bool, st *panelState) {
	sectionBlock(c, first, func() {
		spec := sectionSpec{title: l.Text(l10n.KeyProviderCopilot), refreshing: s.Refreshing}
		if p.CopilotMonitoringEnabled {
			spec.refresh = sectionRefresh(acts, runway.SectionQuota)
		}
		drawSectionHead(c, l, st, spec)
		if !p.CopilotMonitoringEnabled {
			dimText(c, l.Copilot("hint"))
			if accentButton(c, icPlug, l.Copilot("enable"), acts.SetCopilotMonitoring == nil).Clicked() && acts.SetCopilotMonitoring != nil {
				acts.SetCopilotMonitoring(true)
			}
			return
		}
		if s.Stale {
			dimText(c, l.Copilot("stale"))
		}
		copilotAuthorizationButton(c, l, s, acts.AuthorizeCopilot)
		ui.Column(c).Gap(12).Children(func() {
			for _, m := range s.Meters {
				grokMeterRow(c.Key("copilot-"+m.Title), l, m, true, time.Now(), st.resetSwitch(p.QuotaResetTimeFormat))
			}
			for _, line := range s.Lines {
				ui.Row(c).Gap(8).Children(func() {
					ui.Text(c, line.Title).FontSize(fsCaption).Grow(1).Basis(0)
					ui.Text(c, line.Value).FontSize(fsCaption).FontFeatures("tnum")
				})
			}
		})
		if len(s.Meters) == 0 && len(s.Lines) == 0 && s.LastError == "" {
			sectionEmpty(c, l, s.Refreshing)
		}
		if !s.UpdatedAt.IsZero() {
			dimText(c, l.Text(l10n.KeyLastUpdated)+" · "+s.UpdatedAt.In(time.Local).Format("2006-01-02 15:04:05"))
		}
		if linkButton(c, l.Copilot("open")).Clicked() {
			c.OpenURL(copilotUsageURL)
		}
	})
}
func copilotAccountKind() int { return len(store.Providers()) + 1 }
func renderCopilotAccountsPage(c *ui.Context, u *settingsUI, f *accountForm, rt *liveApp) bool {
	if f.kind != copilotAccountKind() {
		return false
	}
	s, p, l := u.s, u.s.Prefs(), u.L()
	state := rt.svc.Snapshot().Copilot
	settingsGroup(c, l.Text(l10n.KeyProviderCopilot), "", nil, func(row rowFunc) {
		row(l.Copilot("enable"), l.Copilot("hint"), func() {
			if v, ok := toggle(c, "copilot-monitor", l.Copilot("enable"), p.CopilotMonitoringEnabled, s.ReadOnly()); ok {
				s.SetCopilotMonitoringEnabled(v)
			}
		})
		row(l.Copilot("source"), "", func() { copilotSourceControl(c, s, state.Refreshing) })
	})
	if !p.CopilotMonitoringEnabled {
		return true
	}
	settingsSection(c, l.Text(l10n.KeyAccountsCurrent), "", func() {
		if ghostButton(c, icRefresh, l.Text(l10n.KeyRefresh), state.Refreshing, false).Clicked() {
			rt.svc.RefreshCopilot(true)
		}
	}, func() {
		copilotIdentityRow(c, l, p, state)
		summary := runway.CopilotStatusText(l, state)
		if state.LastError != "" {
			summary = state.LastError
		}
		dimText(c, accountUpdatedSummary(l, summary, state.UpdatedAt))
		copilotAuthorizationButton(c, l, state, func() { rt.svc.AuthorizeCopilot() })
		if len(state.Accounts) > 1 || len(state.Accounts) == 1 && p.CopilotAccountID != "" && p.CopilotAccountID != state.Accounts[0].ID {
			dimText(c, l.Copilot("choose"))
			for _, account := range state.Accounts {
				if ghostButton(c, icPlug, accountPrivate(p.AccountPrivacyModeEnabled, account.Login), false, s.ReadOnly() || state.Refreshing).Clicked() {
					s.SetCopilotAccount(account.ID)
				}
			}
		}
		if linkButton(c, l.Copilot("open")).Clicked() {
			c.OpenURL(copilotUsageURL)
		}
	})
	return true
}

func copilotSourceControl(c *ui.Context, s *Settings, refreshing bool) {
	l := s.L()
	names := map[string]string{"cli": "Copilot CLI", "vscode": "VS Code", "vscode-insiders": "VS Code Insiders"}
	source, changed := choose(c, "copilot-source", l.Copilot("source"), s.Prefs().CopilotCredentialSource(), []string{"cli", "vscode", "vscode-insiders"}, func(v string) string { return names[v] }, s.ReadOnly() || refreshing)
	if changed {
		s.SetCopilotSource(source)
	}
}

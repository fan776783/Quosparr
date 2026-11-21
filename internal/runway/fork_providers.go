package runway

import (
	"context"
	"time"

	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/store"
)

// ForkProviders groups the fork's quota-only sources without adding them to
// upstream's account providers. Their refreshes never initiate authorization.
type ForkProviders struct {
	Copilot CopilotProvider
	Cursor  CursorProvider
	Droid   QuotaOnlyProvider
	Devin   QuotaOnlyProvider
}

// ForkSnapshot carries the fork's immutable views in snapshots and compact
// status inputs. Each source owns the same read-only slice contract as before.
type ForkSnapshot struct {
	Copilot CopilotState
	Cursor  CursorState
	Droid   QuotaOnlyState
	Devin   QuotaOnlyState
}

func (s *Service) initForkProviders() {
	if c := s.opts.Copilot; c != nil {
		c.SetOnChange(s.publish)
	}
	if c := s.opts.Cursor; c != nil {
		c.SetOnChange(s.publish)
	}
	for _, q := range []QuotaOnlyProvider{s.opts.Droid, s.opts.Devin} {
		if q != nil {
			q.SetOnChange(s.publish)
		}
	}
}

func (s *Service) fillForkSnapshotLocked(snap *Snapshot, in *StatusInputs, now time.Time) {
	if c := s.opts.Copilot; c != nil {
		snap.Copilot = c.State(now)
	}
	if c := s.opts.Cursor; c != nil {
		snap.Cursor = c.State(now)
	}
	if q := s.opts.Droid; q != nil {
		snap.Droid = q.State(now)
	}
	if q := s.opts.Devin; q != nil {
		snap.Devin = q.State(now)
	}
	in.ForkSnapshot = snap.ForkSnapshot
}

type forkQuotaWanted struct{ copilot, cursor, droid, devin bool }

// Called under s.mu alongside the upstream provider selection. Capturing both
// sides of a preference update preserves the newly-visible refresh semantics.
func (s *Service) forkQuotaWantedLocked() forkQuotaWanted {
	return forkQuotaWanted{
		copilot: s.quotaWantedLocked(store.ProviderCopilot),
		cursor:  s.quotaWantedLocked(store.ProviderCursor),
		droid:   s.quotaWantedLocked(store.ProviderDroid),
		devin:   s.quotaWantedLocked(store.ProviderDevin),
	}
}

func (s *Service) refreshForkPreferences(old, next store.Preferences, before, after forkQuotaWanted) {
	if !before.copilot && after.copilot || next.CopilotMonitoringEnabled && (old.CopilotSource != next.CopilotSource || old.CopilotAccountID != next.CopilotAccountID) {
		s.RefreshCopilot(false)
	}
	if !before.cursor && after.cursor {
		s.RefreshCursor(false)
	}
	if !before.droid && after.droid {
		s.RefreshQuotaOnly(store.ProviderDroid, false)
	}
	if !before.devin && after.devin {
		s.RefreshQuotaOnly(store.ProviderDevin, false)
	}
}

func (s *Service) refreshForkQuota(ctx context.Context, wanted forkQuotaWanted) {
	if c := s.opts.Copilot; c != nil && wanted.copilot {
		_ = c.Refresh(ctx, false)
	}
	// A forced refresh of another provider must not bypass Cursor's interval.
	if c := s.opts.Cursor; c != nil && wanted.cursor {
		_ = c.Refresh(ctx, false)
	}
	if q := s.opts.Droid; q != nil && wanted.droid {
		_ = q.Refresh(ctx, false)
	}
	if q := s.opts.Devin; q != nil && wanted.devin {
		_ = q.Refresh(ctx, false)
	}
}

func (s *Service) refreshForkQuotaSection() {
	// Preserve RefreshQuota's existing Cursor-only background refresh. Copilot
	// has its own explicit section action and the scheduled full refresh.
	s.RefreshCursor(false)
	s.RefreshQuotaOnly(store.ProviderDroid, false)
	s.RefreshQuotaOnly(store.ProviderDevin, false)
}

func (s *Service) forkDueResetLocked(after, now time.Time) (time.Time, bool) {
	var meters []Meter
	switch s.prefs.SelectedProvider {
	case store.ProviderCopilot:
		if s.opts.Copilot != nil && s.prefs.CopilotMonitoringEnabled {
			meters = s.opts.Copilot.State(now).LiveMeters()
		}
	case store.ProviderCursor:
		if s.opts.Cursor != nil && s.prefs.CursorMonitoringEnabled {
			meters = s.opts.Cursor.State(now).Meters
		}
	case store.ProviderDroid, store.ProviderDevin:
		if q := s.quotaOnly(s.prefs.SelectedProvider); q != nil && s.prefs.MonitoringEnabled(s.prefs.SelectedProvider) {
			meters = q.State(now).Meters
		}
	default:
		return time.Time{}, false
	}
	var due time.Time
	for _, m := range meters {
		r := m.ResetsAt
		if r.After(after) && now.Sub(r) >= time.Second && (due.IsZero() || r.Before(due)) {
			due = r
		}
	}
	return due, true
}

func (s *Service) nextDueProviderResetLocked(after, now time.Time) time.Time {
	if reset, handled := s.forkDueResetLocked(after, now); handled {
		return reset
	}
	return s.nextDueResetLocked(after, now)
}

// The status item dispatch of the fork's providers, consulted before the
// upstream switches; ok is false for an upstream provider.

func forkStatusText(lc l10n.Localizer, in StatusInputs, p store.Provider) (string, bool) {
	switch p {
	case store.ProviderCopilot:
		return CopilotStatusText(lc, in.Copilot), true
	case store.ProviderCursor:
		return CursorStatusText(lc, in.Cursor), true
	case store.ProviderDroid:
		return QuotaOnlyStatusText(lc, in.Droid, in.Now), true
	case store.ProviderDevin:
		return QuotaOnlyStatusText(lc, in.Devin, in.Now), true
	}
	return "", false
}

func forkMeters(in StatusInputs, p store.Provider) ([]Meter, bool) {
	switch p {
	case store.ProviderCopilot:
		return in.Copilot.LiveMeters(), true
	case store.ProviderCursor:
		return in.Cursor.LiveMeters(), true
	case store.ProviderDroid:
		return in.Droid.Meters, true
	case store.ProviderDevin:
		return in.Devin.Meters, true
	}
	return nil, false
}

func forkPreferred(p store.Provider) ([]int, bool) {
	switch p {
	case store.ProviderCopilot, store.ProviderCursor:
		return nil, true
	case store.ProviderDroid, store.ProviderDevin:
		return quotaOnlyPreferredWindows, true
	}
	return nil, false
}

func forkStatusKey(p store.Provider) (l10n.Key, bool) {
	switch p {
	case store.ProviderCopilot:
		return l10n.KeyProviderCopilot, true
	case store.ProviderCursor:
		return l10n.KeyProviderCursor, true
	case store.ProviderDroid:
		return l10n.KeyProviderDroid, true
	case store.ProviderDevin:
		return l10n.KeyProviderDevin, true
	}
	return "", false
}

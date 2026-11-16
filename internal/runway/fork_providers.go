package runway

import (
	"context"
	"time"

	"github.com/licoy/quosparr/internal/store"
)

// ForkProviders groups the fork's quota-only sources without adding them to
// upstream's account providers. Their refreshes never initiate authorization.
type ForkProviders struct {
	Copilot CopilotProvider
	Cursor  CursorProvider
}

// ForkSnapshot carries the fork's immutable views in snapshots and compact
// status inputs. Each source owns the same read-only slice contract as before.
type ForkSnapshot struct {
	Copilot CopilotState
	Cursor  CursorState
}

func (s *Service) initForkProviders() {
	if c := s.opts.Copilot; c != nil {
		c.SetOnChange(s.publish)
	}
	if c := s.opts.Cursor; c != nil {
		c.SetOnChange(s.publish)
	}
}

func (s *Service) fillForkSnapshotLocked(snap *Snapshot, in *StatusInputs, now time.Time) {
	if c := s.opts.Copilot; c != nil {
		snap.Copilot = c.State(now)
	}
	if c := s.opts.Cursor; c != nil {
		snap.Cursor = c.State(now)
	}
	in.ForkSnapshot = snap.ForkSnapshot
}

type forkQuotaWanted struct{ copilot, cursor bool }

// Called under s.mu alongside the upstream provider selection. Capturing both
// sides of a preference update preserves the newly-visible refresh semantics.
func (s *Service) forkQuotaWantedLocked() forkQuotaWanted {
	return forkQuotaWanted{
		copilot: s.quotaWantedLocked(store.ProviderCopilot),
		cursor:  s.quotaWantedLocked(store.ProviderCursor),
	}
}

func (s *Service) refreshForkPreferences(old, next store.Preferences, before, after forkQuotaWanted) {
	if !before.copilot && after.copilot || next.CopilotMonitoringEnabled && (old.CopilotSource != next.CopilotSource || old.CopilotAccountID != next.CopilotAccountID) {
		s.RefreshCopilot(false)
	}
	if !before.cursor && after.cursor {
		s.RefreshCursor(false)
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
}

func (s *Service) refreshForkQuotaSection() {
	// Preserve RefreshQuota's existing Cursor-only background refresh. Copilot
	// has its own explicit section action and the scheduled full refresh.
	s.RefreshCursor(false)
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

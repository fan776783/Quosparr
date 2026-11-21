package runway

import (
	"context"
	"time"

	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/store"
)

// DailyMinutes is the length of Devin's daily window.
const DailyMinutes = 24 * 60

// quotaOnlyPreferredWindows put the shortest standard window of Droid and
// Devin first.
var quotaOnlyPreferredWindows = []int{FiveHourMinutes, DailyMinutes, WeeklyMinutes}

// QuotaOnlyAvailability is the state of Droid or Devin.
type QuotaOnlyAvailability int

// QuotaOnlyAvailability values.
const (
	QuotaOnlyDisabled QuotaOnlyAvailability = iota
	QuotaOnlyLoading
	QuotaOnlyReady
	QuotaOnlyNotInstalled
	QuotaOnlyNotLoggedIn
	// QuotaOnlyReauthRequired: the login expired or was rejected; the
	// provider's own client has to renew it.
	QuotaOnlyReauthRequired
	QuotaOnlyNoSubscription
	// QuotaOnlyUnsupported: the login is of a kind without a subscription
	// quota.
	QuotaOnlyUnsupported
	QuotaOnlyRateLimited
	QuotaOnlyFailed
)

// QuotaOnlyState is the view of Droid or Devin. It holds no credential
// material; a failed refresh keeps the last reading.
type QuotaOnlyState struct {
	Availability QuotaOnlyAvailability
	// Name and Email identify the signed-in account, "" when unknown.
	Name, Email string
	// Plan is the subscription name, "" when unknown.
	Plan      string
	UpdatedAt time.Time
	// Meters are the quota bars, the standard windows first.
	Meters []Meter
	// Details are the extra rows of the info popover.
	Details    []DetailLine
	LastError  string
	Refreshing bool
}

// QuotaOnlyProvider follows the lock rules of ClaudeProvider. There is no
// RefreshLocal: neither integration reads local sessions.
type QuotaOnlyProvider interface {
	State(time.Time) QuotaOnlyState
	Refresh(context.Context, bool) error
	SetOnChange(func())
}

func (s *Service) quotaOnly(p store.Provider) QuotaOnlyProvider {
	switch p {
	case store.ProviderDroid:
		return s.opts.Droid
	case store.ProviderDevin:
		return s.opts.Devin
	}
	return nil
}

// RefreshQuotaOnly refreshes Droid or Devin while its monitoring is on; a
// non-forced refresh also needs the provider to be shown.
func (s *Service) RefreshQuotaOnly(p store.Provider, force bool) <-chan struct{} {
	s.mu.Lock()
	q := s.quotaOnly(p)
	wanted := s.prefs.MonitoringEnabled(p) && (force || s.quotaWantedLocked(p))
	s.mu.Unlock()
	if q == nil || !wanted {
		return closedChan()
	}
	return s.run(func() { _ = q.Refresh(s.root, force); s.publish() })
}

// QuotaOnlyStatusText is the status text of Droid or Devin: the countdown or
// remaining percentage of the preferred standard meter, else the state.
func QuotaOnlyStatusText(lc l10n.Localizer, g QuotaOnlyState, now time.Time) string {
	m, ok := PrimaryStandardMeter(g.Meters, quotaOnlyPreferredWindows)
	if !ok {
		switch g.Availability {
		case QuotaOnlyDisabled:
			return lc.Text(l10n.KeyQuotaOnlyOff)
		case QuotaOnlyNotLoggedIn, QuotaOnlyReauthRequired:
			return lc.Text(l10n.KeyStatusLogin)
		case QuotaOnlyLoading:
			return lc.Text(l10n.KeyStatusWait)
		case QuotaOnlyReady:
			return lc.Text(l10n.KeyStatusUnknown)
		}
		return lc.Text(l10n.KeyStatusError)
	}
	if m.HasReset() {
		return DurationLocalized(m.ResetsAt.Sub(now).Seconds(), lc.Language(), false)
	}
	return percentText(m)
}

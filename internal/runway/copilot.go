package runway

import (
	"context"
	"time"

	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/store"
)

type CopilotAvailability int

const (
	CopilotDisabled CopilotAvailability = iota
	CopilotLoading
	CopilotReady
	CopilotNotLoggedIn
	CopilotReauthRequired
	CopilotFailed
)

// CopilotState describes the current desktop account without switching or CLI actions.
// It contains no credential material; IdentityID is an opaque generation key.
type CopilotState struct {
	Availability            CopilotAvailability
	IdentityID, Email, Plan string
	Accounts                []CopilotAccount
	Meters                  []Meter
	Lines                   []DetailLine
	UpdatedAt               time.Time
	Refreshing, Stale       bool
	NeedsAuthorization      bool
	LastError               string
}

// LiveMeters are the meters compact views may show: a stale reading is kept
// for the panel but never shown as a current percentage elsewhere.
func (s CopilotState) LiveMeters() []Meter {
	if s.Stale {
		return nil
	}
	return s.Meters
}

// CopilotProvider follows the same lock rules as ClaudeProvider. There is no
// RefreshLocal: this integration does not read editor conversations.
type CopilotProvider interface {
	State(time.Time) CopilotState
	Refresh(context.Context, bool) error
	SetOnChange(func())
}

func (s *Service) RefreshCopilot(force bool) <-chan struct{} {
	s.mu.Lock()
	c := s.opts.Copilot
	wanted := s.prefs.CopilotMonitoringEnabled && (force || s.quotaWantedLocked(store.ProviderCopilot))
	s.mu.Unlock()
	if c == nil || !wanted {
		return closedChan()
	}
	return s.run(func() { _ = c.Refresh(s.root, force); s.publish() })
}

// AuthorizeCopilot is a foreground action, separate from all refresh paths.
func (s *Service) AuthorizeCopilot() <-chan struct{} {
	s.mu.Lock()
	c, ok := s.opts.Copilot.(interface{ Authorize(context.Context) error })
	wanted := s.prefs.CopilotMonitoringEnabled
	s.mu.Unlock()
	if !ok || !wanted {
		return closedChan()
	}
	return s.run(func() { _ = c.Authorize(s.root); s.publish() })
}

func CopilotStatusText(l l10n.Localizer, s CopilotState) string {
	if s.Stale {
		return l.Text(l10n.KeyStatusError)
	}
	if len(s.Meters) > 0 {
		return percentText(s.Meters[0])
	}
	switch s.Availability {
	case CopilotDisabled:
		return l.Text(l10n.KeyCopilotOff)
	case CopilotLoading:
		return l.Text(l10n.KeyStatusWait)
	case CopilotNotLoggedIn, CopilotReauthRequired:
		return l.Text(l10n.KeyStatusLogin)
	case CopilotReady:
		return l.Text(l10n.KeyStatusUnknown)
	default:
		return l.Text(l10n.KeyStatusError)
	}
}

// CopilotAccount contains only display identity, never credentials.
type CopilotAccount struct{ ID, Login string }

package app

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/licoy/quosparr/internal/copilot"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/runway"
	"github.com/licoy/quosparr/internal/store"
)

type copilotBridge struct {
	mu                               sync.Mutex
	gate                             chan struct{}
	enabled, refreshing              bool
	generation                       uint64
	cancel                           context.CancelFunc
	source, account, identity, login string
	accounts                         []runway.CopilotAccount
	quota                            *copilot.Snapshot
	err                              error
	nextPoll, retryAt                time.Time
	lc                               l10n.Localizer
	onChange                         func()
	reader                           *copilot.Reader
	read                             func(context.Context, string) ([]copilot.Auth, error)
	authorize                        func(context.Context, string) ([]copilot.Auth, error)
	fetch                            func(context.Context, copilot.Auth) (copilot.Snapshot, error)
	now                              func() time.Time
}

func newCopilotBridge(home string, p store.Preferences, langs []string) *copilotBridge {
	r := copilot.NewReader(home)
	if dir := os.Getenv("COPILOT_HOME"); filepath.IsAbs(dir) {
		r.CLIDir = dir
	}
	b := &copilotBridge{gate: make(chan struct{}, 1), reader: r, read: r.Read, authorize: r.Authorize, fetch: (copilot.Client{}).FetchAccount, now: time.Now}
	b.setPreferences(p, langs)
	return b
}
func (b *copilotBridge) SetOnChange(fn func()) { b.mu.Lock(); b.onChange = fn; b.mu.Unlock() }
func (b *copilotBridge) notify() {
	b.mu.Lock()
	fn := b.onChange
	b.mu.Unlock()
	if fn != nil {
		fn()
	}
}
func (b *copilotBridge) setPreferences(p store.Preferences, langs []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lc = l10n.ForPreference(string(p.Language), langs)
	if b.enabled == p.CopilotMonitoringEnabled && b.source == p.CopilotCredentialSource() && b.account == p.CopilotAccountID {
		return
	}
	clearReader := !p.CopilotMonitoringEnabled || b.source != p.CopilotCredentialSource()
	b.enabled, b.source, b.account = p.CopilotMonitoringEnabled, p.CopilotCredentialSource(), p.CopilotAccountID
	b.generation++
	if b.cancel != nil {
		b.cancel()
	}
	b.quota, b.err, b.accounts = nil, nil, nil
	b.identity, b.login = "", ""
	b.refreshing = false
	b.nextPoll, b.retryAt = time.Time{}, time.Time{}
	// Preferences are applied by the serial preference worker.
	if clearReader {
		b.reader.Clear()
	}
}
func (b *copilotBridge) close() {
	b.mu.Lock()
	b.enabled = false
	b.generation++
	if b.cancel != nil {
		b.cancel()
	}
	b.mu.Unlock()
	b.reader.Clear()
}
func (b *copilotBridge) Refresh(ctx context.Context, force bool) error {
	return b.refresh(ctx, force, false)
}
func (b *copilotBridge) Authorize(ctx context.Context) error { return b.refresh(ctx, true, true) }
func (b *copilotBridge) refresh(parent context.Context, force, authorize bool) error {
	select {
	case b.gate <- struct{}{}:
		defer func() { <-b.gate }()
	case <-parent.Done():
		return parent.Err()
	}
	b.mu.Lock()
	if !b.enabled {
		b.mu.Unlock()
		return nil
	}
	gen, source, selected := b.generation, b.source, b.account
	timeout := 30 * time.Second
	if authorize {
		timeout = 2 * time.Minute
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	b.cancel = cancel
	b.refreshing = true
	b.mu.Unlock()
	b.notify()
	defer func() {
		cancel()
		b.mu.Lock()
		if gen == b.generation {
			b.refreshing = false
			b.cancel = nil
		}
		b.mu.Unlock()
		b.notify()
	}()
	var auths []copilot.Auth
	var err error
	if authorize {
		auths, err = b.authorize(ctx, source)
	} else {
		auths, err = b.read(ctx, source)
	}
	b.mu.Lock()
	if gen != b.generation || !b.enabled {
		b.mu.Unlock()
		return nil
	}
	if err != nil {
		b.err = err
		if errors.Is(err, copilot.ErrLogin) {
			b.reader.Clear()
			b.quota = nil
			b.identity, b.login = "", ""
			b.accounts = nil
		}
		b.mu.Unlock()
		return err
	}
	b.accounts = nil
	var auth copilot.Auth
	for _, a := range auths {
		b.accounts = append(b.accounts, runway.CopilotAccount{ID: a.ID, Login: a.Login})
		if a.ID == selected {
			auth = a
		}
	}
	if selected == "" && len(auths) == 1 {
		auth = auths[0]
	}
	if auth.Token == "" {
		b.quota = nil
		b.identity, b.login = "", ""
		b.err = copilot.ErrAccount
		b.mu.Unlock()
		return copilot.ErrAccount
	}
	id := source + ":" + auth.ID
	if id != b.identity {
		b.quota = nil
		b.err = nil
		b.nextPoll = time.Time{}
		b.retryAt = time.Time{}
	}
	b.identity, b.login = id, auth.Login
	// Read the login before applying account-scoped cooldowns, so an external
	// switch or logout cannot inherit another account's schedule or quota.
	if b.now().Before(b.retryAt) || !force && b.now().Before(b.nextPoll) {
		b.mu.Unlock()
		return nil
	}
	b.nextPoll = b.now().Add(5 * time.Minute)
	b.mu.Unlock()
	q, err := b.fetch(ctx, auth)
	// Reject results if the source login changed during the request.
	if ctx.Err() == nil {
		current, e := b.read(ctx, source)
		same := false
		for _, a := range current {
			same = same || a.ID == auth.ID && a.Token == auth.Token
		}
		if e != nil || !same {
			err = copilot.ErrLogin
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if gen != b.generation || !b.enabled || ctx.Err() == context.Canceled {
		return nil
	}
	b.err = err
	if err == nil {
		b.quota = &q
		b.retryAt = time.Time{}
	} else {
		if errors.Is(err, copilot.ErrLogin) {
			b.reader.Clear()
			b.identity, b.login = "", ""
			b.accounts = nil
		}
		if errors.Is(err, copilot.ErrUnauthorized) {
			b.reader.Clear()
		}
		if errors.Is(err, copilot.ErrUnauthorized) || errors.Is(err, copilot.ErrLogin) || errors.Is(err, copilot.ErrBilling) {
			b.quota = nil
		}
		var limited *copilot.RateLimitedError
		if errors.As(err, &limited) {
			b.retryAt = b.now().Add(limited.RetryAfter)
		}
	}
	return err
}
func (b *copilotBridge) State(now time.Time) runway.CopilotState {
	b.mu.Lock()
	defer b.mu.Unlock()
	st := runway.CopilotState{Availability: runway.CopilotDisabled}
	if !b.enabled {
		return st
	}
	st.Availability = runway.CopilotLoading
	st.Refreshing = b.refreshing
	st.IdentityID, st.Email = b.identity, b.login
	st.Accounts = append([]runway.CopilotAccount(nil), b.accounts...)
	l := b.lc
	if q := b.quota; q != nil {
		st.Availability = runway.CopilotReady
		st.UpdatedAt = q.UpdatedAt
		st.Plan = q.Plan
		st.Stale = now.Sub(q.UpdatedAt) > 15*time.Minute
		if q.PercentRemaining != nil {
			m := runway.NewMeter("AI Credits", runway.MeterWindow{UsedPercent: int(math.Round(100 - *q.PercentRemaining)), ResetsAt: q.ResetsAt}, now, nil, runway.SourceStandard)
			m.PreserveTitle = true
			m.Projection = nil
			st.Meters = []runway.Meter{m}
		}
		for _, v := range []struct {
			key   string
			value *float64
		}{{"remaining", q.Remaining}, {"total", q.Limit}, {"used", q.Used}, {"overage", q.Overage}} {
			if v.value != nil {
				st.Lines = append(st.Lines, runway.DetailLine{Title: l.Copilot(v.key), Value: fmt.Sprintf("%.2f AIC", *v.value)})
			}
		}
		if q.Unlimited {
			st.Lines = append(st.Lines, runway.DetailLine{Title: "AI Credits", Value: l.Copilot("shared")})
		}
		if q.Unlimited && !q.HasQuota || q.PercentRemaining != nil && *q.PercentRemaining == 0 {
			st.Lines = append(st.Lines, runway.DetailLine{Title: "AI Credits", Value: l.Copilot("exhausted")})
		}
		key := "off"
		if q.OveragePermitted {
			key = "enabled"
		}
		st.Lines = append(st.Lines, runway.DetailLine{Title: l.Copilot("additional"), Value: l.Copilot(key)})
	}
	if b.err != nil {
		st.Stale = b.quota != nil
		st.Availability = runway.CopilotFailed
		key := "failed"
		var limited *copilot.RateLimitedError
		switch {
		case errors.Is(b.err, copilot.ErrAuthorize):
			key = "authorize"
			st.NeedsAuthorization = true
		case errors.Is(b.err, copilot.ErrAccount):
			key = "choose"
		case errors.Is(b.err, copilot.ErrLogin):
			key = "login"
			st.Availability = runway.CopilotNotLoggedIn
		case errors.Is(b.err, copilot.ErrBilling):
			key = "billing"
		case errors.Is(b.err, copilot.ErrUnauthorized):
			key = "reauth"
			st.NeedsAuthorization = true
			st.Availability = runway.CopilotReauthRequired
		case errors.Is(b.err, copilot.ErrUnsupported):
			key = "unsupported"
		case errors.Is(b.err, copilot.ErrCredentials):
			key = "credentials"
		case errors.As(b.err, &limited):
			key = "limited"
		}
		st.LastError = l.Copilot(key)
	}
	return st
}

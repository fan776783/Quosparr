package app

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"sync"
	"time"

	"github.com/licoy/quosparr/internal/devin"
	"github.com/licoy/quosparr/internal/droid"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/runway"
	"github.com/licoy/quosparr/internal/store"
)

// quotaOnlyBackoff spaces automatic reads after the service rate limited one.
const quotaOnlyBackoff = 5 * time.Minute

// quotaOnlySource is the provider half of a quota-only bridge. fetch keeps
// the last reading and returns a renderer of it that no later fetch changes;
// the bridge serializes fetch and never holds its state lock across it.
type quotaOnlySource interface {
	fetch(ctx context.Context, now time.Time) (quotaOnlyRender, error)
	// reset forgets the last reading; it never runs alongside fetch.
	reset()
}

// quotaOnlyRender builds the state of a reading in a language.
type quotaOnlyRender func(lc l10n.Localizer) runway.QuotaOnlyState

// quotaOnlyBridge adapts Droid or Devin to runway.QuotaOnlyProvider. It reads
// nothing while the provider's monitoring is off, and drops its reading when
// monitoring is turned off.
type quotaOnlyBridge struct {
	provider store.Provider
	src      quotaOnlySource
	enabled  bool
	gen      uint64
	// resetSrc asks the next refresh to forget the source's reading; only a
	// refresh, which holds refreshMu, touches the source.
	resetSrc  bool
	now       func() time.Time
	refreshMu sync.Mutex
	mu        sync.Mutex
	lc        l10n.Localizer
	state     runway.QuotaOnlyState
	render    quotaOnlyRender
	refreshes int
	retryAt   time.Time
	onChange  func()
}

func newQuotaOnlyBridge(provider store.Provider, src quotaOnlySource, prefs store.Preferences, langs []string) *quotaOnlyBridge {
	b := &quotaOnlyBridge{provider: provider, src: src, now: time.Now}
	b.setPreferences(prefs, langs)
	return b
}

func newDroidBridge(home string, prefs store.Preferences, langs []string) *quotaOnlyBridge {
	return newQuotaOnlyBridge(store.ProviderDroid, &droidSource{store: droid.NewStore(home)}, prefs, langs)
}

func newDevinBridge(home, version string, prefs store.Preferences, langs []string) *quotaOnlyBridge {
	src := &devinSource{dir: devin.DataDir(home, os.Getenv), client: devin.Client{Version: version}}
	return newQuotaOnlyBridge(store.ProviderDevin, src, prefs, langs)
}

func (b *quotaOnlyBridge) State(time.Time) runway.QuotaOnlyState {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.enabled {
		return runway.QuotaOnlyState{Availability: runway.QuotaOnlyDisabled}
	}
	st := b.state
	st.Refreshing = b.refreshes > 0
	return st
}

// Refresh rereads the login and the quota. After a rate limited read only a
// manual refresh reads again before the backoff ends.
func (b *quotaOnlyBridge) Refresh(ctx context.Context, force bool) error {
	b.mu.Lock()
	wait := !b.enabled || !force && b.now().Before(b.retryAt)
	b.mu.Unlock()
	if wait {
		return nil
	}
	b.changeRefreshing(1)
	defer b.changeRefreshing(-1)
	b.refreshMu.Lock()
	defer b.refreshMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	gen, enabled, reset := b.gen, b.enabled, b.resetSrc
	b.resetSrc = false
	b.mu.Unlock()
	if reset {
		b.src.reset()
	}
	if !enabled {
		return nil
	}
	render, err := b.src.fetch(ctx, b.now())
	b.mu.Lock()
	if gen != b.gen {
		// Monitoring was turned off meanwhile: drop the reading.
		b.mu.Unlock()
		return nil
	}
	if errors.Is(err, droid.ErrRateLimited) || errors.Is(err, devin.ErrRateLimited) {
		b.retryAt = b.now().Add(quotaOnlyBackoff)
	} else if err == nil {
		b.retryAt = time.Time{}
	}
	b.render = render
	b.state = render(b.lc)
	fn := b.onChange
	b.mu.Unlock()
	if fn != nil {
		fn()
	}
	return ctx.Err()
}

func (b *quotaOnlyBridge) SetOnChange(fn func()) {
	b.mu.Lock()
	b.onChange = fn
	b.mu.Unlock()
}

func (b *quotaOnlyBridge) changeRefreshing(delta int) {
	b.mu.Lock()
	b.refreshes += delta
	fn := b.onChange
	b.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// setPreferences relabels the reading in the new language and follows the
// monitoring switch; turning it off forgets the reading.
func (b *quotaOnlyBridge) setPreferences(p store.Preferences, langs []string) {
	b.mu.Lock()
	lc := l10n.ForPreference(string(p.Language), langs)
	enabled := p.MonitoringEnabled(b.provider)
	changed := lc.Language() != b.lc.Language() || enabled != b.enabled
	b.lc = lc
	if enabled != b.enabled {
		b.enabled = enabled
		b.gen++
		b.render, b.retryAt = nil, time.Time{}
		b.state = runway.QuotaOnlyState{Availability: runway.QuotaOnlyLoading}
		b.resetSrc = true
	}
	if changed && b.render != nil {
		b.state = b.render(lc)
	}
	fn := b.onChange
	b.mu.Unlock()
	if changed && fn != nil {
		fn()
	}
}

// droidSource reads the droid CLI login and its billing limits.
type droidSource struct {
	store  droid.Store
	client droid.Client
	login  *droid.Login
	limits *droid.Limits
	err    error
}

func (s *droidSource) fetch(ctx context.Context, now time.Time) (quotaOnlyRender, error) {
	err := s.read(ctx, now)
	view := *s
	return view.state, err
}

func (s *droidSource) reset() { s.login, s.limits, s.err = nil, nil, nil }

func (s *droidSource) read(ctx context.Context, now time.Time) error {
	login, err := s.store.Load(ctx, now)
	if err != nil && !errors.Is(err, droid.ErrLoginExpired) {
		if ctx.Err() == nil {
			s.login, s.limits, s.err = nil, nil, err
		}
		return err
	}
	if s.login == nil || s.login.Email != login.Email {
		s.limits = nil
	}
	s.login = &login
	if err == nil {
		var limits droid.Limits
		if limits, err = s.client.Fetch(ctx, login, now); err == nil {
			s.limits = &limits
		}
	}
	if ctx.Err() == nil {
		s.err = err
	}
	return err
}

func (s droidSource) state(lc l10n.Localizer) runway.QuotaOnlyState {
	st := runway.QuotaOnlyState{Availability: runway.QuotaOnlyLoading}
	if s.login != nil {
		st.Name, st.Email = s.login.Name, s.login.Email
	}
	st.Availability, st.LastError = droidAvailability(lc, s.err, s.limits != nil)
	if s.limits == nil {
		return st
	}
	l := *s.limits
	st.UpdatedAt = l.UpdatedAt
	st.Plan = droid.PlanName(l.Plan)
	st.Meters = droidMeters(lc, l)
	if st.Plan != "" {
		st.Details = append(st.Details, runway.DetailLine{Title: lc.Text(l10n.KeyPlan), Value: st.Plan})
	}
	if l.ExtraUsageBalanceCents != nil {
		st.Details = append(st.Details, runway.DetailLine{Title: lc.Text(l10n.KeyDroidExtraUsage), Value: fmt.Sprintf("$%.2f", float64(*l.ExtraUsageBalanceCents)/100)})
	}
	return st
}

func droidAvailability(lc l10n.Localizer, err error, reading bool) (runway.QuotaOnlyAvailability, string) {
	switch {
	case err == nil && reading:
		return runway.QuotaOnlyReady, ""
	case err == nil:
		return runway.QuotaOnlyLoading, ""
	case errors.Is(err, droid.ErrNotInstalled):
		return runway.QuotaOnlyNotInstalled, lc.Text(l10n.KeyDroidNotInstalled)
	case errors.Is(err, droid.ErrNotLoggedIn):
		return runway.QuotaOnlyNotLoggedIn, lc.Text(l10n.KeyDroidNotLoggedIn)
	case errors.Is(err, droid.ErrLoginExpired):
		return runway.QuotaOnlyReauthRequired, lc.Text(l10n.KeyDroidLoginExpired)
	case errors.Is(err, droid.ErrUnauthorized):
		return runway.QuotaOnlyReauthRequired, lc.Text(l10n.KeyDroidLoginRejected)
	case errors.Is(err, droid.ErrNoSubscription):
		return runway.QuotaOnlyNoSubscription, lc.Text(l10n.KeyDroidNoSubscription)
	case errors.Is(err, droid.ErrRateLimited):
		return runway.QuotaOnlyRateLimited, lc.Text(l10n.KeyQuotaOnlyRateLimited)
	case errors.Is(err, droid.ErrLoginUnreadable):
		return runway.QuotaOnlyFailed, lc.Text(l10n.KeyDroidLoginUnreadable)
	case errors.Is(err, droid.ErrBadResponse):
		return runway.QuotaOnlyFailed, lc.Text(l10n.KeyQuotaOnlyParseFailed)
	}
	return runway.QuotaOnlyFailed, lc.Text(l10n.KeyQuotaOnlyRefreshFailed)
}

// droidMeters are the Standard windows, then the Core ones as model-specific
// meters.
func droidMeters(lc l10n.Localizer, l droid.Limits) []runway.Meter {
	meters := droidPoolMeters(lc, lc.Text(l10n.KeyDroidStandard), l.Standard, runway.SourceStandard, l.UpdatedAt)
	if l.Core != nil {
		meters = append(meters, droidPoolMeters(lc, lc.Text(l10n.KeyDroidCore), *l.Core, runway.SourceModelSpecific, l.UpdatedAt)...)
	}
	return meters
}

func droidPoolMeters(lc l10n.Localizer, pool string, p droid.Pool, source runway.MeterSource, now time.Time) []runway.Meter {
	var meters []runway.Meter
	for _, w := range []struct {
		window  *droid.Window
		minutes int
	}{{p.FiveHour, runway.FiveHourMinutes}, {p.Weekly, runway.WeeklyMinutes}, {p.Monthly, runway.MonthlyMinutes}} {
		if w.window == nil {
			continue
		}
		window := runway.MeterWindow{UsedPercent: int(math.Round(w.window.UsedPercent)), WindowMinutes: w.minutes, ResetsAt: w.window.EndsAt}
		title := pool + " · " + runway.ShortWindowTitle(lc, w.minutes)
		meters = append(meters, runway.NewMeter(title, window, now, runway.DefaultMarkers(), source))
	}
	return meters
}

// devinSource reads the Devin CLI login and its user status.
type devinSource struct {
	dir    string
	client devin.Client
	key    string
	status *devin.Status
	err    error
}

func (s *devinSource) fetch(ctx context.Context, now time.Time) (quotaOnlyRender, error) {
	err := s.read(ctx, now)
	view := *s
	return view.state, err
}

func (s *devinSource) reset() { s.key, s.status, s.err = "", nil, nil }

func (s *devinSource) read(ctx context.Context, now time.Time) error {
	login, err := devin.Load(s.dir)
	if err != nil {
		s.key, s.status, s.err = "", nil, err
		return err
	}
	if login.APIKey != s.key {
		s.status = nil
	}
	s.key = login.APIKey
	status, err := s.client.Fetch(ctx, login, now)
	if err == nil {
		s.status = &status
	}
	if ctx.Err() == nil {
		s.err = err
	}
	return err
}

func (s devinSource) state(lc l10n.Localizer) runway.QuotaOnlyState {
	st := runway.QuotaOnlyState{}
	st.Availability, st.LastError = devinAvailability(lc, s.err, s.status != nil)
	if s.status == nil {
		return st
	}
	d := *s.status
	st.Name, st.Email, st.Plan, st.UpdatedAt = d.Name, d.Email, d.Plan, d.UpdatedAt
	for _, w := range []struct {
		window  *devin.Window
		minutes int
		title   l10n.Key
	}{{d.Daily, runway.DailyMinutes, l10n.KeyHeatmapDaily}, {d.Weekly, runway.WeeklyMinutes, l10n.KeyWeeklyUsage}} {
		if w.window == nil {
			continue
		}
		window := runway.MeterWindow{UsedPercent: 100 - w.window.RemainingPercent, WindowMinutes: w.minutes, ResetsAt: w.window.ResetsAt}
		st.Meters = append(st.Meters, runway.NewMeter(lc.Text(w.title), window, d.UpdatedAt, runway.DefaultMarkers(), runway.SourceStandard))
	}
	if d.Plan != "" {
		st.Details = append(st.Details, runway.DetailLine{Title: lc.Text(l10n.KeyPlan), Value: d.Plan})
	}
	return st
}

func devinAvailability(lc l10n.Localizer, err error, reading bool) (runway.QuotaOnlyAvailability, string) {
	switch {
	case err == nil && reading:
		return runway.QuotaOnlyReady, ""
	case err == nil:
		return runway.QuotaOnlyLoading, ""
	case errors.Is(err, devin.ErrNotInstalled):
		return runway.QuotaOnlyNotInstalled, lc.Text(l10n.KeyDevinNotInstalled)
	case errors.Is(err, devin.ErrNotLoggedIn):
		return runway.QuotaOnlyNotLoggedIn, lc.Text(l10n.KeyDevinNotLoggedIn)
	case errors.Is(err, devin.ErrUnsupportedLogin):
		return runway.QuotaOnlyUnsupported, lc.Text(l10n.KeyDevinUnsupportedLogin)
	case errors.Is(err, devin.ErrUnauthorized):
		return runway.QuotaOnlyReauthRequired, lc.Text(l10n.KeyDevinLoginRejected)
	case errors.Is(err, devin.ErrRateLimited):
		return runway.QuotaOnlyRateLimited, lc.Text(l10n.KeyQuotaOnlyRateLimited)
	case errors.Is(err, devin.ErrLoginUnreadable):
		return runway.QuotaOnlyFailed, lc.Text(l10n.KeyDevinLoginUnreadable)
	case errors.Is(err, devin.ErrBadResponse):
		return runway.QuotaOnlyFailed, lc.Text(l10n.KeyQuotaOnlyParseFailed)
	}
	return runway.QuotaOnlyFailed, lc.Text(l10n.KeyQuotaOnlyRefreshFailed)
}

package app

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/licoy/quosparr/internal/devin"
	"github.com/licoy/quosparr/internal/droid"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/pricing"
	"github.com/licoy/quosparr/internal/runway"
	"github.com/licoy/quosparr/internal/store"
	"github.com/licoy/quosparr/internal/usage"
)

// quotaOnlyBackoff spaces automatic reads after the service rate limited one.
const quotaOnlyBackoff = 5 * time.Minute

// quotaOnlySource is the provider half of a quota-only bridge. fetch keeps
// the last reading and returns a renderer of it that no later fetch changes;
// the bridge serializes fetch and never holds its state lock across it.
type quotaOnlySource interface {
	fetch(ctx context.Context, now time.Time) (quotaOnlyRender, error)
	// costDetail returns the last quota detail snapshot used to derive weekly
	// cost cycles; never runs alongside fetch.
	costDetail() any
	// reset forgets the last reading; it never runs alongside fetch.
	reset()
}

// quotaOnlyRender builds the state of a reading in a language.
type quotaOnlyRender func(lc l10n.Localizer) runway.QuotaOnlyState

type quotaOnlyCostCache struct {
	valid          bool
	fingerprint    string
	windowKey      string
	pricingVersion string
	windowCosts    []runway.QuotaOnlyWindowCost
}

type quotaOnlyWindowTarget struct {
	source        runway.MeterSource
	windowMinutes int
	start         time.Time
}

// quotaOnlyBridge adapts Droid or Devin to runway.QuotaOnlyProvider. It reads
// nothing while the provider's monitoring is off, and drops its reading when
// monitoring is turned off.
type quotaOnlyBridge struct {
	provider        store.Provider
	src             quotaOnlySource
	sessionsDir     string
	dataDir         string
	pricingSnapshot func() pricing.Snapshot
	eventsScanner   func(ctx context.Context, w usage.Window, now time.Time) ([]usage.PricedEvent, string, error)
	enabled         bool
	showsCost       bool
	gen             uint64
	pricingGen      uint64
	// resetSrc asks the next refresh to forget the source's reading; only a
	// refresh, which holds refreshMu, touches the source.
	resetSrc  bool
	now       func() time.Time
	refreshMu sync.Mutex
	costMu    sync.Mutex
	mu        sync.Mutex
	lc        l10n.Localizer
	state     runway.QuotaOnlyState
	render    quotaOnlyRender
	detail    any
	costs     []runway.QuotaOnlyWindowCost
	costCache quotaOnlyCostCache
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
	b := newQuotaOnlyBridge(store.ProviderDroid, &droidSource{store: droid.NewStore(home)}, prefs, langs)
	b.sessionsDir = filepath.Join(home, ".factory", "sessions")
	return b
}

func newDevinBridge(home, version string, prefs store.Preferences, langs []string) *quotaOnlyBridge {
	dir := devin.DataDir(home, os.Getenv)
	src := &devinSource{dir: dir, client: devin.Client{Version: version}}
	b := newQuotaOnlyBridge(store.ProviderDevin, src, prefs, langs)
	b.dataDir = dir
	return b
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

// Refresh rereads the login and the quota, publishes the quota immediately,
// and then scans local session cost when enabled. After a rate limited read
// only a manual refresh reads the quota again before the backoff ends.
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
	if err := ctx.Err(); err != nil {
		b.refreshMu.Unlock()
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
		b.refreshMu.Unlock()
		return nil
	}
	render, err := b.src.fetch(ctx, b.now())
	detail := b.src.costDetail()
	b.refreshMu.Unlock()

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
	b.detail = detail
	st := render(b.lc)
	if b.showsCost && len(b.costs) > 0 {
		st.WindowCosts = append([]runway.QuotaOnlyWindowCost(nil), b.costs...)
	}
	b.state = st
	fn := b.onChange
	b.mu.Unlock()
	if fn != nil {
		fn()
	}
	_ = b.scanCost(ctx, false)
	return ctx.Err()
}

// RefreshLocal rescans local session cost without touching the quota API.
func (b *quotaOnlyBridge) RefreshLocal(ctx context.Context) error {
	b.mu.Lock()
	enabled := b.enabled && b.showsCost
	b.mu.Unlock()
	if !enabled {
		return nil
	}
	b.changeRefreshing(1)
	defer b.changeRefreshing(-1)
	if err := ctx.Err(); err != nil {
		return err
	}
	return b.scanCost(ctx, true)
}

func (b *quotaOnlyBridge) scanCost(ctx context.Context, force bool) error {
	b.costMu.Lock()
	defer b.costMu.Unlock()

	b.mu.Lock()
	if !b.enabled || !b.showsCost {
		b.mu.Unlock()
		return nil
	}
	gen := b.gen
	priceGen := b.pricingGen
	meters := append([]runway.Meter(nil), b.state.Meters...)
	snapFn := b.pricingSnapshot
	cache := b.costCache
	b.mu.Unlock()

	now := b.now()
	targets, earliest, ok := quotaOnlyWindowTargets(meters, now)
	if !ok {
		b.mu.Lock()
		if gen == b.gen && priceGen == b.pricingGen && b.enabled && b.showsCost && ctx.Err() == nil && len(b.costs) > 0 {
			b.costs = nil
			b.state.WindowCosts = nil
			fn := b.onChange
			b.mu.Unlock()
			if fn != nil {
				fn()
			}
		} else {
			b.mu.Unlock()
		}
		return nil
	}

	var snap pricing.Snapshot
	if snapFn != nil {
		snap = snapFn()
	}
	priceVer := usage.VendorPricingVersion("", snap)
	fp := b.costFingerprint()
	winKey := quotaOnlyWindowTargetKey(targets)
	if !force && fp != "" && cache.valid && cache.fingerprint == fp && cache.windowKey == winKey && cache.pricingVersion == priceVer {
		reused := append([]runway.QuotaOnlyWindowCost(nil), cache.windowCosts...)
		b.mu.Lock()
		if gen == b.gen && priceGen == b.pricingGen && b.enabled && b.showsCost && ctx.Err() == nil {
			b.costs = reused
			b.state.WindowCosts = append([]runway.QuotaOnlyWindowCost(nil), reused...)
			fn := b.onChange
			b.mu.Unlock()
			if fn != nil {
				fn()
			}
		} else {
			b.mu.Unlock()
		}
		return nil
	}

	events, _, err := b.scanCostEvents(ctx, usage.Window{Start: earliest, End: now}, now, snapFn)
	if err != nil {
		return err
	}
	costs := summarizeWindowCosts(targets, events, now)
	b.mu.Lock()
	if gen != b.gen || priceGen != b.pricingGen || !b.enabled || !b.showsCost || ctx.Err() != nil {
		b.mu.Unlock()
		return ctx.Err()
	}
	if snapFn != nil && usage.VendorPricingVersion("", snapFn()) != priceVer {
		b.mu.Unlock()
		return context.Canceled
	}
	b.costs = costs
	b.state.WindowCosts = append([]runway.QuotaOnlyWindowCost(nil), costs...)
	if fp != "" {
		b.costCache = quotaOnlyCostCache{
			valid:          true,
			fingerprint:    fp,
			windowKey:      winKey,
			pricingVersion: priceVer,
			windowCosts:    append([]runway.QuotaOnlyWindowCost(nil), costs...),
		}
	}
	fn := b.onChange
	b.mu.Unlock()
	if fn != nil {
		fn()
	}
	return nil
}

func quotaOnlyWindowTargets(meters []runway.Meter, now time.Time) (targets []quotaOnlyWindowTarget, earliest time.Time, ok bool) {
	for _, m := range meters {
		if m.Source != runway.SourceStandard || m.WindowMinutes <= 0 || !m.HasReset() {
			continue
		}
		start := m.ResetsAt.Add(-time.Duration(m.WindowMinutes) * time.Minute)
		targets = append(targets, quotaOnlyWindowTarget{
			source:        m.Source,
			windowMinutes: m.WindowMinutes,
			start:         start,
		})
		if start.Before(now) && (earliest.IsZero() || start.Before(earliest)) {
			earliest = start
		}
	}
	if len(targets) == 0 || earliest.IsZero() || !now.After(earliest) {
		return nil, time.Time{}, false
	}
	return targets, earliest, true
}

func quotaOnlyWindowTargetKey(targets []quotaOnlyWindowTarget) string {
	var b strings.Builder
	for i, t := range targets {
		if i > 0 {
			b.WriteByte('|')
		}
		b.WriteString(strconv.Itoa(int(t.source)))
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(t.windowMinutes))
		b.WriteByte(':')
		b.WriteString(strconv.FormatInt(t.start.UnixNano(), 10))
	}
	return b.String()
}

func summarizeWindowCosts(targets []quotaOnlyWindowTarget, events []usage.PricedEvent, now time.Time) []runway.QuotaOnlyWindowCost {
	out := make([]runway.QuotaOnlyWindowCost, len(targets))
	for i, t := range targets {
		wc := runway.QuotaOnlyWindowCost{
			Source:        t.source,
			WindowMinutes: t.windowMinutes,
		}
		if now.After(t.start) {
			for _, ev := range events {
				if ev.Totals.TotalTokens <= 0 || ev.Time.Before(t.start) || !ev.Time.Before(now) {
					continue
				}
				if ev.USD != nil {
					wc.USD = wc.USD.Add(*ev.USD)
					wc.Priced = true
				} else {
					wc.Partial = true
				}
			}
		}
		out[i] = wc
	}
	return out
}

func (b *quotaOnlyBridge) costFingerprint() string {
	if b.provider == store.ProviderDevin && b.dataDir != "" {
		st, ok := devin.SessionDBState(b.dataDir)
		if !ok {
			return "missing"
		}
		return fmt.Sprintf("%d:%d:%d:%d", st.DBSize, st.DBMtime.UnixNano(), st.WALSize, st.WALMtime.UnixNano())
	}
	return ""
}

func (b *quotaOnlyBridge) scanCostEvents(ctx context.Context, w usage.Window, now time.Time, snapFn func() pricing.Snapshot) ([]usage.PricedEvent, string, error) {
	if b.eventsScanner != nil {
		return b.eventsScanner(ctx, w, now)
	}
	if snapFn == nil {
		snapFn = b.pricingSnapshot
	}
	if b.provider == store.ProviderDevin {
		return (devin.SessionScanner{
			DataDir:         b.dataDir,
			Location:        time.Local,
			PricingSnapshot: snapFn,
		}).ScanEvents(ctx, w, now)
	}
	return (droid.SessionScanner{
		Dir:             b.sessionsDir,
		Location:        time.Local,
		PricingSnapshot: snapFn,
	}).ScanEvents(ctx, w, now)
}

func (b *quotaOnlyBridge) scanCostWindow(ctx context.Context, w usage.Window, now time.Time, snapFn func() pricing.Snapshot) (usage.Summary, error) {
	if snapFn == nil {
		snapFn = b.pricingSnapshot
	}
	if b.eventsScanner != nil {
		events, ver, err := b.eventsScanner(ctx, w, now)
		if err != nil {
			return usage.Summary{}, err
		}
		client := droid.SessionClientName
		if b.provider == store.ProviderDevin {
			client = devin.SessionClientName
		}
		return usage.SummarizePricedEvents(events, w, time.Local, client, ver, now), nil
	}
	if b.provider == store.ProviderDevin {
		return (devin.SessionScanner{
			DataDir:         b.dataDir,
			Location:        time.Local,
			PricingSnapshot: snapFn,
		}).ScanCost(ctx, w, now)
	}
	return (droid.SessionScanner{
		Dir:             b.sessionsDir,
		Location:        time.Local,
		PricingSnapshot: snapFn,
	}).ScanCost(ctx, w, now)
}

func (b *quotaOnlyBridge) costDetail() any {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.detail
}

func (b *quotaOnlyBridge) invalidateCost() {
	b.mu.Lock()
	b.pricingGen++
	b.costs = nil
	b.costCache = quotaOnlyCostCache{}
	b.state.WindowCosts = nil
	fn := b.onChange
	b.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// quotaOnlyCostWindow resolves the cost window for Droid or Devin: Today and
// ThisMonth follow the local calendar; Current and Previous use the active
// weekly quota reset (Standard.Weekly for Droid, Weekly for Devin) when it is
// still in the future, falling back to the trailing 7-day / prior 7-day window.
func quotaOnlyCostWindow(provider store.Provider, r store.CostRange, detail any, now time.Time) usage.Window {
	switch r {
	case store.CostRangeToday:
		return usage.Today(now, time.Local).Window
	case store.CostRangeThisMonth:
		return usage.ThisMonth(now, time.Local).Window
	}
	var resetAt time.Time
	switch provider {
	case store.ProviderDroid:
		if limits, ok := detail.(*droid.Limits); ok && limits != nil && limits.Standard.Weekly != nil {
			resetAt = limits.Standard.Weekly.EndsAt
		}
	case store.ProviderDevin:
		if status, ok := detail.(*devin.Status); ok && status != nil && status.Weekly != nil {
			resetAt = status.Weekly.ResetsAt
		}
	}
	if !resetAt.IsZero() && resetAt.After(now) {
		end := resetAt
		start := end.Add(-7 * 24 * time.Hour)
		if r == store.CostRangePrevious {
			return usage.Window{Start: start.Add(-7 * 24 * time.Hour), End: start}
		}
		return usage.Window{Start: start, End: minTime(end, now)}
	}
	start := now.Add(-7 * 24 * time.Hour)
	if r == store.CostRangePrevious {
		return usage.Window{Start: start.Add(-7 * 24 * time.Hour), End: start}
	}
	return usage.Window{Start: start, End: now}
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
	costToggled := b.showsCost != p.ShowsCostSummary
	changed := lc.Language() != b.lc.Language() || enabled != b.enabled || costToggled
	b.lc = lc
	b.showsCost = p.ShowsCostSummary
	if enabled != b.enabled {
		b.enabled = enabled
		b.gen++
		b.render, b.retryAt = nil, time.Time{}
		b.detail, b.costs, b.costCache = nil, nil, quotaOnlyCostCache{}
		b.state = runway.QuotaOnlyState{Availability: runway.QuotaOnlyLoading}
		b.resetSrc = true
	} else if costToggled && !p.ShowsCostSummary {
		// Only the cost is stale: a quota read in flight stays valid.
		b.pricingGen++
		b.costs = nil
		b.costCache = quotaOnlyCostCache{}
		b.state.WindowCosts = nil
	}
	if changed && b.render != nil {
		st := b.render(lc)
		if b.showsCost && len(b.costs) > 0 {
			st.WindowCosts = append([]runway.QuotaOnlyWindowCost(nil), b.costs...)
		}
		b.state = st
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

func (s *droidSource) costDetail() any {
	if s.limits == nil {
		return nil
	}
	cp := *s.limits
	return &cp
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

func (s *devinSource) costDetail() any {
	if s.status == nil {
		return nil
	}
	cp := *s.status
	return &cp
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

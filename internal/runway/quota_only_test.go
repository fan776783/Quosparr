package runway

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/store"
	"github.com/licoy/quosparr/internal/usage"
)

type fakeQuotaOnly struct {
	mu       sync.Mutex
	state    QuotaOnlyState
	forced   []bool
	onChange func()
}

func (q *fakeQuotaOnly) State(time.Time) QuotaOnlyState {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.state
}
func (q *fakeQuotaOnly) Refresh(_ context.Context, force bool) error {
	q.mu.Lock()
	q.forced = append(q.forced, force)
	q.mu.Unlock()
	return nil
}
func (q *fakeQuotaOnly) SetOnChange(fn func()) { q.mu.Lock(); q.onChange = fn; q.mu.Unlock() }
func (q *fakeQuotaOnly) calls() []bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]bool(nil), q.forced...)
}

func TestQuotaOnlyStatusText(t *testing.T) {
	lc := l10n.New(l10n.English)
	now := time.Date(2026, 11, 21, 8, 0, 0, 0, time.UTC)
	weekly := Meter{WindowMinutes: WeeklyMinutes, Source: SourceStandard, RemainingPercent: 40}
	daily := Meter{WindowMinutes: DailyMinutes, Source: SourceStandard, RemainingPercent: 70}
	core := Meter{WindowMinutes: FiveHourMinutes, Source: SourceModelSpecific, RemainingPercent: 10}
	if got := QuotaOnlyStatusText(lc, QuotaOnlyState{Meters: []Meter{weekly, daily, core}}, now); got != "70%" {
		t.Fatalf("daily before weekly, standard only: %q", got)
	}
	for av, key := range map[QuotaOnlyAvailability]l10n.Key{
		QuotaOnlyNotLoggedIn: l10n.KeyStatusLogin, QuotaOnlyReauthRequired: l10n.KeyStatusLogin,
		QuotaOnlyLoading: l10n.KeyStatusWait, QuotaOnlyReady: l10n.KeyStatusUnknown, QuotaOnlyNotInstalled: l10n.KeyStatusError,
	} {
		if got := QuotaOnlyStatusText(lc, QuotaOnlyState{Availability: av}, now); got != lc.Text(key) {
			t.Errorf("%v: %q", av, got)
		}
	}
	if got := QuotaOnlyStatusText(lc, QuotaOnlyState{}, now); got != lc.Text(l10n.KeyQuotaOnlyOff) {
		t.Fatalf("disabled: %q", got)
	}
}

func TestQuotaOnlyProvidersRefreshAndShow(t *testing.T) {
	f := newFake(t)
	d := &fakeQuotaOnly{state: QuotaOnlyState{Availability: QuotaOnlyReady, Plan: "Team",
		Meters: []Meter{{WindowMinutes: FiveHourMinutes, Source: SourceStandard, RemainingPercent: 77}}}}
	v := &fakeQuotaOnly{state: QuotaOnlyState{Availability: QuotaOnlyNotLoggedIn}}
	s, _ := newService(t, f, func(o *Options) {
		o.Droid, o.Devin = d, v
		o.Preferences.SelectedProvider = store.ProviderDroid
		o.Preferences.DroidMonitoringEnabled = true
		o.Preferences.DevinMonitoringEnabled = true
	})
	wait(t, s.Refresh(usage.RefreshForce))
	settle(t, s)
	if got := d.calls(); len(got) != 1 || got[0] {
		t.Fatalf("droid full refresh calls %v", got)
	}
	if got := v.calls(); len(got) != 1 {
		t.Fatalf("devin full refresh calls %v", got)
	}
	snap := s.Snapshot()
	if snap.Droid.Plan != "Team" || snap.Devin.Availability != QuotaOnlyNotLoggedIn {
		t.Fatalf("snapshot droid %+v devin %+v", snap.Droid, snap.Devin)
	}
	if c := snap.StatusBar.Components; len(c) != 1 || c[0].Provider != store.ProviderDroid || c[0].Cells[0].Remaining != 77 {
		t.Fatalf("status bar %+v", c)
	}
	wait(t, s.RefreshQuotaOnly(store.ProviderDroid, true))
	if got := d.calls(); len(got) != 2 || !got[1] {
		t.Fatalf("section refresh calls %v", got)
	}
	ws := s.WidgetSnapshot(f.clk.Now())
	for _, p := range ws.Providers {
		if p.Provider == "droid" || p.Provider == "devin" {
			t.Fatalf("quota-only provider in widgets: %+v", p)
		}
	}
}

func TestQuotaOnlyMonitoringOffNeverReads(t *testing.T) {
	f := newFake(t)
	d := &fakeQuotaOnly{}
	s, _ := newService(t, f, func(o *Options) {
		o.Droid = d
		o.Preferences.SelectedProvider = store.ProviderDroid
	})
	wait(t, s.Refresh(usage.RefreshForce))
	wait(t, s.RefreshQuotaOnly(store.ProviderDroid, true))
	settle(t, s)
	if got := d.calls(); len(got) != 0 {
		t.Fatalf("disabled Droid read %v", got)
	}
	if got := s.Snapshot().StatusBar.Components[0].Cells[0]; !got.Empty {
		t.Fatalf("disabled status cell %+v", got)
	}
}

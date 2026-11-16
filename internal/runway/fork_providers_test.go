package runway

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/licoy/quosparr/internal/store"
)

type forkGemini struct{}

func (*forkGemini) State(time.Time) GeminiState {
	return GeminiState{Availability: GeminiReady, Meters: []Meter{{Source: SourceStandard, WindowMinutes: FiveHourMinutes, RemainingPercent: 67}}}
}
func (*forkGemini) Refresh(context.Context, bool) error { return nil }
func (*forkGemini) RefreshLocal(context.Context) error  { return nil }
func (*forkGemini) SetOnChange(func())                  {}

// Shared wiring must retain both the upstream provider and the fork's
// quota-only providers when their adjacent initialization blocks change.
func TestForkProvidersCoexistWithGemini(t *testing.T) {
	s, _ := newService(t, newFake(t), func(o *Options) {
		o.Gemini = &forkGemini{}
		o.Cursor = &fakeCursor{}
		o.Copilot = &fakeCopilot{}
	})
	p := s.Preferences()
	p.SelectedProvider = store.ProviderGemini
	s.SetPreferences(p)
	settle(t, s)
	snap := s.Snapshot()
	if snap.Gemini.Availability != GeminiReady || snap.Cursor.Availability != CursorReady || snap.Copilot.Availability != CopilotReady {
		t.Fatal("snapshot lost an upstream or fork provider")
	}
	if len(snap.StatusBar.Components) != 1 || snap.StatusBar.Components[0].Cells[0].Percent != "67%" {
		t.Fatal("Gemini status bar lost its provider state")
	}
}

type forkCursorProbe struct {
	fakeCursor
	state      CursorState
	forced     atomic.Int32
	authorized atomic.Int32
}

func (c *forkCursorProbe) State(time.Time) CursorState { return c.state }
func (c *forkCursorProbe) Refresh(ctx context.Context, force bool) error {
	if force {
		c.forced.Add(1)
	}
	return c.fakeCursor.Refresh(ctx, force)
}
func (c *forkCursorProbe) Authorize(context.Context) error {
	c.authorized.Add(1)
	return nil
}

type forkCopilotProbe struct {
	fakeCopilot
	state CopilotState
}

func (c *forkCopilotProbe) State(time.Time) CopilotState { return c.state }

func TestForkRefreshKeepsAuthorizationAndForceBoundaries(t *testing.T) {
	cursor := &forkCursorProbe{state: CursorState{Availability: CursorReady}}
	copilot := &fakeCopilot{}
	s, _ := newService(t, newFake(t), func(o *Options) {
		o.Cursor, o.Copilot = cursor, copilot
		o.Preferences.CursorMonitoringEnabled = true
		o.Preferences.CopilotMonitoringEnabled = true
	})
	refreshAll(t, s) // A forced full refresh still respects Cursor's interval.
	if cursor.count.Load() != 1 || copilot.count.Load() != 1 || cursor.forced.Load() != 0 || cursor.authorized.Load() != 0 {
		t.Fatal("full refresh changed fork refresh or authorization semantics")
	}
	wait(t, s.RefreshCursor(true))
	if cursor.forced.Load() != 1 || cursor.authorized.Load() != 0 {
		t.Fatal("explicit refresh must force quota without authorizing")
	}
}

func TestForkQuotaResetKeepsProviderSemantics(t *testing.T) {
	now := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	after := now.Add(-time.Hour)
	meters := []Meter{
		{ResetsAt: now.Add(time.Hour)},
		{ResetsAt: now.Add(-500 * time.Millisecond)},
		{ResetsAt: now.Add(-2 * time.Minute)},
		{ResetsAt: now.Add(-3 * time.Minute)},
	}
	for _, provider := range []store.Provider{store.ProviderCursor, store.ProviderCopilot} {
		for _, enabled := range []bool{false, true} {
			for _, stale := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/enabled=%t/stale=%t", provider, enabled, stale), func(t *testing.T) {
					s, _ := newService(t, newFake(t), func(o *Options) {
						o.Cursor = &forkCursorProbe{state: CursorState{Meters: meters, Stale: stale}}
						o.Copilot = &forkCopilotProbe{state: CopilotState{Meters: meters, Stale: stale}}
						o.Preferences.SelectedProvider = provider
						o.Preferences.CursorMonitoringEnabled = enabled
						o.Preferences.CopilotMonitoringEnabled = enabled
					})
					var want time.Time
					if enabled && (provider == store.ProviderCursor || !stale) {
						want = now.Add(-3 * time.Minute)
					}
					if got := s.NextDueQuotaReset(after, now); !got.Equal(want) {
						t.Fatalf("due reset = %v, want %v", got, want)
					}
				})
			}
		}
	}
}

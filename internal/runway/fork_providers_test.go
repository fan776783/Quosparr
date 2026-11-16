package runway

import (
	"context"
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

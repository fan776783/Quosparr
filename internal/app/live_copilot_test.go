package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/licoy/quosparr/internal/copilot"
	"github.com/licoy/quosparr/internal/runway"
	"github.com/licoy/quosparr/internal/store"
)

func copilotTestBridge(t *testing.T) (*copilotBridge, *atomic.Int32) {
	t.Helper()
	p := store.DefaultPreferences()
	p.CopilotMonitoringEnabled = true
	p.Language = store.LanguageEnglish
	b := newCopilotBridge(t.TempDir(), p, nil)
	t.Cleanup(b.close)
	b.now = func() time.Time { return time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC) }
	b.read = func(context.Context, string) ([]copilot.Auth, error) {
		return []copilot.Auth{{ID: "7", Login: "synthetic", Host: "https://github.com", Token: "synthetic-access"}}, nil
	}
	calls := &atomic.Int32{}
	b.fetch = func(context.Context, copilot.Auth) (copilot.Snapshot, error) {
		calls.Add(1)
		remaining := 89.5
		return copilot.Snapshot{PercentRemaining: &remaining, HasQuota: true, UpdatedAt: b.now()}, nil
	}
	return b, calls
}
func TestCopilotOptInRefreshRateAndRevocation(t *testing.T) {
	b, calls := copilotTestBridge(t)
	p := store.DefaultPreferences()
	b.setPreferences(p, nil)
	reads := 0
	read := b.read
	b.read = func(ctx context.Context, s string) ([]copilot.Auth, error) { reads++; return read(ctx, s) }
	b.Refresh(context.Background(), true)
	if reads != 0 || calls.Load() != 0 {
		t.Fatal("disabled provider read credentials")
	}
	p.CopilotMonitoringEnabled = true
	b.setPreferences(p, nil)
	if e := b.Refresh(context.Background(), false); e != nil {
		t.Fatal(e)
	}
	b.Refresh(context.Background(), false)
	st := b.State(b.now())
	if calls.Load() != 1 || len(st.Meters) != 1 || st.Meters[0].RemainingPercent != 89 {
		t.Fatal("quota conversion or poll minimum lost")
	}
	if !b.State(b.now().Add(16 * time.Minute)).Stale {
		t.Fatal("old data remained live")
	}
	b.fetch = func(context.Context, copilot.Auth) (copilot.Snapshot, error) {
		calls.Add(1)
		return copilot.Snapshot{}, &copilot.RateLimitedError{RetryAfter: time.Hour}
	}
	b.Refresh(context.Background(), true)
	b.Refresh(context.Background(), true)
	st = b.State(b.now())
	if calls.Load() != 2 || !st.Stale || len(st.LiveMeters()) != 0 {
		t.Fatal("Retry-After or stale isolation failed")
	}
	p.CopilotMonitoringEnabled = false
	b.setPreferences(p, nil)
	st = b.State(b.now())
	if st.IdentityID != "" || len(st.Meters) != 0 || len(st.Accounts) != 0 || st.Availability != runway.CopilotDisabled {
		t.Fatal("disable retained account data")
	}
}
func TestCopilotMultipleAccountsAndExternalSwitch(t *testing.T) {
	b, calls := copilotTestBridge(t)
	b.read = func(context.Context, string) ([]copilot.Auth, error) {
		return []copilot.Auth{{ID: "7", Login: "one", Token: "synthetic-one"}, {ID: "8", Login: "two", Token: "synthetic-two"}}, nil
	}
	if e := b.Refresh(context.Background(), true); !errors.Is(e, copilot.ErrAccount) || calls.Load() != 0 {
		t.Fatal("first saved account selected silently")
	}
	p := store.DefaultPreferences()
	p.CopilotMonitoringEnabled = true
	p.CopilotAccountID = "8"
	b.setPreferences(p, nil)
	if e := b.Refresh(context.Background(), true); e != nil || b.State(b.now()).Email != "two" {
		t.Fatal("explicit account selection failed")
	}
	reads := 0
	b.read = func(context.Context, string) ([]copilot.Auth, error) {
		reads++
		token := "before"
		if reads > 1 {
			token = "after"
		}
		return []copilot.Auth{{ID: "8", Login: "two", Token: token}}, nil
	}
	if e := b.Refresh(context.Background(), true); !errors.Is(e, copilot.ErrLogin) || len(b.State(b.now()).LiveMeters()) != 0 {
		t.Fatal("old credential response survived rotation")
	}
}
func TestCopilotSourceChangeDiscardsInFlightResponse(t *testing.T) {
	b, _ := copilotTestBridge(t)
	started, release := make(chan struct{}), make(chan struct{})
	b.fetch = func(ctx context.Context, _ copilot.Auth) (copilot.Snapshot, error) {
		close(started)
		<-release
		p := 10.0
		return copilot.Snapshot{PercentRemaining: &p, UpdatedAt: b.now()}, nil
	}
	done := make(chan struct{})
	go func() { b.Refresh(context.Background(), true); close(done) }()
	<-started
	p := store.DefaultPreferences()
	p.CopilotMonitoringEnabled = true
	p.CopilotSource = "vscode"
	b.setPreferences(p, nil)
	close(release)
	<-done
	if s := b.State(b.now()); len(s.Meters) != 0 || s.IdentityID != "" {
		t.Fatal("old source quota published after selection change")
	}
}

func TestCopilotAccountChangeDoesNotInheritPollingOrRateLimit(t *testing.T) {
	for _, limited := range []bool{false, true} {
		t.Run(map[bool]string{false: "minimum-poll", true: "rate-limit"}[limited], func(t *testing.T) {
			b, calls := copilotTestBridge(t)
			current := "7"
			b.read = func(context.Context, string) ([]copilot.Auth, error) {
				return []copilot.Auth{{ID: current, Login: "account-" + current, Token: "synthetic-" + current}}, nil
			}
			if e := b.Refresh(context.Background(), false); e != nil {
				t.Fatal(e)
			}
			fetch := b.fetch
			if limited {
				b.fetch = func(context.Context, copilot.Auth) (copilot.Snapshot, error) {
					calls.Add(1)
					return copilot.Snapshot{}, &copilot.RateLimitedError{RetryAfter: time.Hour}
				}
				b.Refresh(context.Background(), true)
			}
			before := calls.Load()
			b.fetch = fetch
			current = "8"
			if e := b.Refresh(context.Background(), false); e != nil {
				t.Fatal(e)
			}
			st := b.State(b.now())
			if calls.Load() != before+1 || st.IdentityID != "cli:8" || st.Email != "account-8" || st.LastError != "" || st.Stale || len(st.Meters) != 1 {
				t.Fatal("new account inherited the previous account's quota schedule or error")
			}
			b.read = func(context.Context, string) ([]copilot.Auth, error) { return nil, copilot.ErrLogin }
			if e := b.Refresh(context.Background(), false); !errors.Is(e, copilot.ErrLogin) {
				t.Fatal("polling cooldown prevented logout detection")
			}
			if st := b.State(b.now()); st.IdentityID != "" || len(st.LiveMeters()) != 0 {
				t.Fatal("logout kept the previous account quota")
			}
		})
	}
}

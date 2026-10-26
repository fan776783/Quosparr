package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/licoy/quosparr/internal/cursor"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/runway"
	"github.com/licoy/quosparr/internal/store"
)

func TestCursorRememberedAuthorizationSurvivesRestart(t *testing.T) {
	e := newEnv(t, nil)
	if err := e.s.Load(); err != nil {
		t.Fatal(err)
	}
	e.s.SetCursorMonitoringEnabled(true)
	e.s.SetCursorRememberAuthorization(true)
	e.settle()
	for range 2 {
		b, calls := cursorTestBridge(t)
		b.setPreferences(e.stored(), nil)
		read := b.read
		authorized, grants := false, 0
		b.read = func(ctx context.Context) (cursor.Auth, error) {
			if !authorized {
				return cursor.Auth{}, cursor.ErrAuthorizationRequired
			}
			return read(ctx)
		}
		b.authorize = func(context.Context) error { grants++; authorized = true; return nil }
		if err := b.Refresh(context.Background(), false); err != nil {
			t.Fatal(err)
		}
		if err := b.Refresh(context.Background(), true); err != nil {
			t.Fatal(err)
		}
		if grants != 1 || calls.Load() != 2 || b.State(b.now()).Availability != runway.CursorReady {
			t.Fatal("remembered consent did not restore encrypted login exactly once")
		}
		// A key change must not create another background permission prompt.
		authorized = false
		_ = b.Refresh(context.Background(), true)
		if grants != 1 || !b.State(b.now()).NeedsAuthorization {
			t.Fatal("key rotation silently reauthorized")
		}
		b.close()
	}
}

func TestCursorRememberedAuthorizationFailureDoesNotLoop(t *testing.T) {
	for _, failure := range []error{cursor.ErrAuthorizationDenied, cursor.ErrKeychainUnavailable, context.DeadlineExceeded} {
		t.Run(failure.Error(), func(t *testing.T) {
			b, calls := cursorTestBridge(t)
			p := store.DefaultPreferences()
			p.CursorMonitoringEnabled, p.CursorRememberAuthorization = true, true
			b.setPreferences(p, nil)
			grants := 0
			b.read = func(context.Context) (cursor.Auth, error) { return cursor.Auth{}, cursor.ErrAuthorizationRequired }
			b.authorize = func(context.Context) error { grants++; return failure }
			for range 3 {
				_ = b.Refresh(context.Background(), true)
			}
			b.setPreferences(p, nil) // unrelated settings save must not reset the attempt
			_ = b.Refresh(context.Background(), false)
			if grants != 1 || calls.Load() != 0 {
				t.Fatal("failed automatic authorization retried or reached API")
			}
			_ = b.Authorize(context.Background())
			if grants != 2 {
				t.Fatal("foreground retry was blocked")
			}
		})
	}
}

func TestCursorRememberConsentWithCachedKeyDoesNotReauthorizeRotation(t *testing.T) {
	b, _ := cursorTestBridge(t)
	p := store.DefaultPreferences()
	p.CursorMonitoringEnabled, p.CursorRememberAuthorization = true, true
	b.setPreferences(p, nil)
	grants := 0
	b.authorize = func(context.Context) error { grants++; return nil }
	if err := b.Refresh(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	b.read = func(context.Context) (cursor.Auth, error) { return cursor.Auth{}, cursor.ErrAuthorizationRequired }
	_ = b.Refresh(context.Background(), true)
	if grants != 0 || !b.State(b.now()).NeedsAuthorization {
		t.Fatal("cached key rotation requested Keychain automatically")
	}
}

func TestCursorRememberedAuthorizationRespectsLoginBoundaries(t *testing.T) {
	for _, failure := range []error{nil, cursor.ErrNotLoggedIn, cursor.ErrUnsupportedLogin, cursor.ErrEncryptedStorage, cursor.ErrCredentials} {
		b, _ := cursorTestBridge(t)
		p := store.DefaultPreferences()
		p.CursorRememberAuthorization = true
		b.setPreferences(p, nil)
		reads, grants := 0, 0
		b.read = func(context.Context) (cursor.Auth, error) { reads++; return cursor.Auth{Token: "synthetic"}, failure }
		b.authorize = func(context.Context) error { grants++; return nil }
		_ = b.Refresh(context.Background(), true)
		if reads != 0 || grants != 0 {
			t.Fatal("remembered consent bypassed monitoring opt-in")
		}
		p.CursorMonitoringEnabled = true
		b.setPreferences(p, nil)
		_ = b.Refresh(context.Background(), true)
		if grants != 0 {
			t.Fatal("plain, missing or unsupported login requested Keychain")
		}
	}
}

func TestCursorRevokeCancelsRememberedAuthorization(t *testing.T) {
	b, calls := cursorTestBridge(t)
	p := store.DefaultPreferences()
	p.CursorMonitoringEnabled, p.CursorRememberAuthorization = true, true
	b.setPreferences(p, nil)
	b.read = func(context.Context) (cursor.Auth, error) { return cursor.Auth{}, cursor.ErrAuthorizationRequired }
	started := make(chan struct{})
	var grants, cleared atomic.Int32
	b.authorize = func(ctx context.Context) error {
		grants.Add(1)
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}
	b.clearAuth = func() { cleared.Add(1) }
	done := make(chan error, 1)
	go func() { done <- b.Refresh(context.Background(), false) }()
	<-started
	p.CursorRememberAuthorization = false
	b.setPreferences(p, nil)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	_ = b.Refresh(context.Background(), true)
	if grants.Load() != 1 || cleared.Load() != 1 || calls.Load() != 0 || !b.State(b.now()).NeedsAuthorization {
		t.Fatal("revocation retained key, prompted again or lost explicit retry")
	}
}

func TestCursorRememberedAuthorizationKeepsRetryAfter(t *testing.T) {
	b, calls := cursorTestBridge(t)
	_ = b.Refresh(context.Background(), true)
	b.retryAt = b.now().Add(time.Minute)
	p := store.DefaultPreferences()
	p.CursorMonitoringEnabled, p.CursorRememberAuthorization = true, true
	b.setPreferences(p, nil)
	read := b.read
	ready := false
	b.read = func(ctx context.Context) (cursor.Auth, error) {
		if !ready {
			return cursor.Auth{}, cursor.ErrAuthorizationRequired
		}
		return read(ctx)
	}
	b.authorize = func(context.Context) error { ready = true; return nil }
	_ = b.Refresh(context.Background(), true)
	if calls.Load() != 1 || b.State(b.now()).NeedsAuthorization || b.State(b.now()).LastError != b.lc.Text(l10n.KeyCursorRateLimited) {
		t.Fatal("automatic authorization bypassed rate limit or retained auth error")
	}
}

func TestCursorAuthorizationChoiceAndPreferenceFailure(t *testing.T) {
	for _, language := range l10n.Languages() {
		l := l10n.New(language)
		o := cursorAuthorizationOptions(l)
		if len(o.Buttons) != 3 || o.DefaultButton != 1 || o.CancelButton != 0 || o.Buttons[1] != l.CursorAuthorization("once") || o.Buttons[2] != l.CursorAuthorization("permanent") {
			t.Fatal("permanent authorization must be an explicit non-default choice")
		}
	}
	e := newEnv(t, nil)
	if err := e.s.Load(); err != nil {
		t.Fatal(err)
	}
	e.mem.fail.Store(true)
	e.s.SetCursorRememberAuthorization(true)
	e.settle()
	if e.s.Prefs().CursorRememberAuthorization || e.stored().CursorRememberAuthorization || e.s.SaveError() == nil {
		t.Fatal("failed consent save was represented as permanent")
	}
}

func TestCursorPermanentChoiceDoesNotRepeatAutomaticDenial(t *testing.T) {
	e := newEnv(t, nil)
	if err := e.s.Load(); err != nil {
		t.Fatal(err)
	}
	e.s.prefs = quietLivePreferences()
	e.s.prefs.SelectedProvider = store.ProviderCursor
	e.s.prefs.PanelProviders = []store.Provider{store.ProviderCursor}
	e.s.SetCursorMonitoringEnabled(true)
	e.settle()
	p := e.s.Prefs()
	b, _ := cursorTestBridge(t)
	b.setPreferences(p, nil)
	var grants atomic.Int32
	b.read = func(context.Context) (cursor.Auth, error) { return cursor.Auth{}, cursor.ErrAuthorizationRequired }
	b.authorize = func(context.Context) error { grants.Add(1); return cursor.ErrAuthorizationDenied }
	svc := runway.New(runway.Options{Preferences: p, Cursor: b})
	defer svc.Close()
	rt := &liveApp{svc: svc, cursorB: b}
	e.s.OnChange(func() {
		b.setPreferences(e.s.Prefs(), nil)
		svc.SetPreferences(e.s.Prefs())
		// Model the automatic attempt finishing before the dialog's worker
		// can dispatch. A second explicit request would prompt twice.
		_ = b.Refresh(context.Background(), true)
	})
	b.promptMu.Lock()
	rt.applyCursorAuthorizationChoice(e.s, true)
	deadline := time.Now().Add(3 * time.Second)
	for {
		e.q.drain()
		if b.promptMu.TryLock() {
			b.promptMu.Unlock()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("authorization worker did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	e.settle()
	if grants.Load() != 1 || !e.stored().CursorRememberAuthorization {
		t.Fatal("permanent choice was lost or duplicated the system prompt")
	}
}

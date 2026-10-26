package app

import (
	"github.com/egoist/mygo"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/store"
)

func (s *Settings) SetCursorRememberAuthorization(on bool) bool {
	return s.Update(func(p *store.Preferences) { p.CursorRememberAuthorization = on })
}

func cursorAuthorizationOptions(l l10n.Localizer) mygo.MessageOptions {
	return mygo.MessageOptions{
		Type: mygo.MessageQuestion, Title: l.CursorAuthorization("title"),
		Message: l.CursorAuthorization("message"), Detail: l.CursorAuthorization("detail"),
		Buttons:       []string{l.Text(l10n.KeyCancel), l.CursorAuthorization("once"), l.CursorAuthorization("permanent")},
		DefaultButton: 1, CancelButton: 0,
	}
}

func (rt *liveApp) requestCursorAuthorization(s *Settings) {
	if s == nil || s.ReadOnly() || !s.Prefs().CursorMonitoringEnabled || rt.cursorB == nil || !rt.cursorB.promptMu.TryLock() {
		return
	}
	l := s.L()
	go func() {
		res, err := mygo.Dialog.Message(cursorAuthorizationOptions(l))
		if err != nil {
			rt.cursorB.promptMu.Unlock()
			runOnMain(func() { ShowDialog(l.CursorAuthorization("title"), l.CursorAuthorization("unavailable")) })
			return
		}
		if res.Button != 1 && res.Button != 2 {
			rt.cursorB.promptMu.Unlock()
			return
		}
		runOnMain(func() {
			rt.applyCursorAuthorizationChoice(s, res.Button == 2)
		})
	}()
}

// Called on the UI thread with promptMu held, transferring its ownership to
// the worker. Settings owns consent persistence and reports any save failure.
func (rt *liveApp) applyCursorAuthorizationChoice(s *Settings, remember bool) {
	wasRemembered := s.Prefs().CursorRememberAuthorization
	if !s.Prefs().CursorMonitoringEnabled || !s.SetCursorRememberAuthorization(remember) {
		rt.cursorB.promptMu.Unlock()
		return
	}
	go func() {
		// Apply revocation before a one-time request so it cannot cancel
		// the request replacing the remembered permission.
		rt.preferenceWork.idle.Wait()
		runOnMain(func() {
			p := s.Prefs()
			if !p.CursorMonitoringEnabled || p.CursorRememberAuthorization != remember {
				rt.cursorB.promptMu.Unlock()
				return
			}
			go func() {
				defer rt.cursorB.promptMu.Unlock()
				if remember && !wasRemembered {
					// Applying new consent may already have started a refresh.
					// Use its one-attempt budget, never a second explicit prompt.
					<-rt.svc.RefreshCursor(true)
				} else {
					<-rt.svc.AuthorizeCursor()
				}
			}()
		})
	}()
}

package app

import "github.com/licoy/quosparr/internal/l10n"

func (rt *liveApp) requestCursorAuthorization(l l10n.Localizer) {
	if rt.cursorB == nil || !rt.cursorB.promptMu.TryLock() {
		return
	}
	go func() {
		defer rt.cursorB.promptMu.Unlock()
		ok, err := confirmDialog(l, l.CursorAuthorization("title"), l.CursorAuthorization("message"), l.CursorAuthorization("detail"), l.CursorAuthorization("action"))
		if err != nil {
			runOnMain(func() { ShowDialog(l.CursorAuthorization("title"), l.CursorAuthorization("unavailable")) })
			return
		}
		if ok {
			<-rt.svc.AuthorizeCursor()
		}
	}()
}

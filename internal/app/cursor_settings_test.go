package app

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"github.com/licoy/quosparr/internal/store"
	"testing"
)

func TestCursorExtendedSettingsLanguages(t *testing.T) {
	for _, language := range []store.Language{store.LanguageTraditionalChinese, store.LanguageKorean, store.LanguageJapanese, store.LanguageFrench} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", language, dark), func(t *testing.T) {
				e := newEnv(t, nil)
				u, tt := openSettingsView(t, e, tabGeneral)
				e.s.SetLanguage(language)
				tt.SetSize(settingsMinWidth, settingsMinHeight)
				tt.SetDark(dark)
				tt.SetTitleBar(ui.TitleBar{Height: 28, Left: 80})
				for _, tab := range settingsTabs {
					u.tab = tab
					tt.Frame()
					for frame := 0; frame < 5; frame++ {
						for _, text := range tt.Texts() {
							r, ok := tt.Find(text)
							if !ok || r.X < -0.5 || r.X+r.W > settingsMinWidth+0.5 {
								t.Fatalf("page %v text %q extends beyond minimum window: %+v", tab, text, r)
							}
						}
						tt.Scroll(650, 450, 0, 350)
					}
				}
				e.settle()
			})
		}
	}
}

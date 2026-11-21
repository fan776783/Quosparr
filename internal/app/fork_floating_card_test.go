package app

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/licoy/quosparr/internal/l10n"
	"github.com/licoy/quosparr/internal/runway"
	"github.com/licoy/quosparr/internal/store"
)

// Every provider at once, each with two windows, still fits a small laptop
// work area in every style.
func TestFloatingEveryProviderFitsASmallScreen(t *testing.T) {
	l := l10n.New(l10n.English)
	meters := floatingSample(l).Providers[0].Meters
	var v runway.FloatingView
	for _, p := range store.DisplayProviders() {
		v.Providers = append(v.Providers, runway.FloatingProvider{Provider: p, Name: l.Text(providerKey(p)), Meters: meters})
	}
	for _, st := range store.FloatingStyles() {
		if w, h := floatingSize(st, v, ""); w > 1280-2*floatingMargin || h > 768-2*floatingMargin {
			t.Errorf("%q: %d providers take %vx%v", st, len(v.Providers), w, h)
		}
	}
}

func TestFloatingCardSharesProvidersOutIntoEvenColumns(t *testing.T) {
	var v runway.FloatingView
	for n, want := range map[int][]int{0: nil, 1: {1}, 4: {4}, 5: {3, 2}, 6: {3, 3}, 7: {4, 3}, 8: {4, 4}, 9: {3, 3, 3}} {
		v.Providers = make([]runway.FloatingProvider, n)
		var got []int
		for _, col := range cardColumns(v) {
			got = append(got, len(col.Providers))
		}
		if !slices.Equal(got, want) {
			t.Errorf("%d providers in columns %v, want %v", n, got, want)
		}
	}
	for _, n := range []int{0, 1, 4} {
		v.Providers = make([]runway.FloatingProvider, n)
		w, h := cardColumnsSize(v, "")
		if uw, uh := cardSize(v, ""); w != uw || h != uh {
			t.Errorf("%d providers: %vx%v, the upstream card is %vx%v", n, w, h, uw, uh)
		}
	}
	v.Providers = make([]runway.FloatingProvider, 8)
	w8, h8 := cardColumnsSize(v, "")
	_, h4 := cardSize(runway.FloatingView{Providers: v.Providers[:4]}, "")
	if w8 != cardW+cardColumnStep || h8 != h4 {
		t.Errorf("two columns of four are %vx%v", w8, h8)
	}
}

func TestFloatingSettingsOffersEveryProvider(t *testing.T) {
	u := cardSettingsUI(t, store.LanguageEnglish)
	l := u.L()
	u.tab = tabFloating
	u.s.SetFloatingWindow(func(f *store.FloatingWindow) {
		f.Providers = []store.Provider{store.ProviderCodex, store.ProviderClaude, store.ProviderGemini}
	})
	tt := ui.NewTester(u.view, settingsWidth, settingsHeight*3)
	for _, p := range store.DisplayProviders()[3:] {
		if err := tt.Click(l.Text(providerKey(p))); err != nil {
			t.Fatal(err)
		}
	}
	if got := u.s.Prefs().FloatingSelection(); !slices.Equal(got, store.DisplayProviders()) {
		t.Fatalf("selection = %v, want every provider", got)
	}
}

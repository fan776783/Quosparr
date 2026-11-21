package app

import (
	"github.com/egoist/mygo/ui"
	"github.com/licoy/quosparr/internal/runway"
)

// cardColumnMax is how many providers the card style stacks in one column;
// the fork shows every display provider, which a single column cannot fit
// on a small screen.
const cardColumnMax = 4

// cardColumns splits the providers into the fewest columns of at most
// cardColumnMax, as even as they go, earlier columns taking the extra one.
func cardColumns(v runway.FloatingView) []runway.FloatingView {
	n := len(v.Providers)
	if n == 0 {
		return nil
	}
	count := (n + cardColumnMax - 1) / cardColumnMax
	per := (n + count - 1) / count
	cols := make([]runway.FloatingView, 0, count)
	for i := 0; i < n; i += per {
		cols = append(cols, runway.FloatingView{Providers: v.Providers[i:min(i+per, n)]})
	}
	return cols
}

// cardColumnStep is the distance between the left edges of two columns: a
// column's content and the gap holding the hairline.
const cardColumnStep = cardW - 2*cardPad + cardGap

// cardColumnsSize is the card size with the providers in columns; one
// column is the upstream card.
func cardColumnsSize(v runway.FloatingView, family string) (float32, float32) {
	cols := cardColumns(v)
	if len(cols) <= 1 {
		return cardSize(v, family)
	}
	var h float32
	for _, col := range cols {
		_, ch := cardSize(col, family)
		h = max(h, ch)
	}
	return cardW + float32(len(cols)-1)*cardColumnStep, h
}

// drawCardColumns draws each column as an upstream card, with a hairline
// between columns.
func drawCardColumns(p *ui.Painter, r ui.Rect, v runway.FloatingView, k floatingInk, family string) {
	for i, col := range cardColumns(v) {
		x := r.X + float32(i)*cardColumnStep
		if i > 0 {
			p.Fill(ui.Rect{X: x + cardPad - cardGap/2 - 0.5, Y: r.Y + cardPad, W: 1, H: r.H - 2*cardPad}, k.track, 0)
		}
		drawCard(p, ui.Rect{X: x, Y: r.Y, W: cardW, H: r.H}, col, k, family)
	}
}

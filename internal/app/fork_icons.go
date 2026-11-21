package app

import (
	_ "embed"
	"github.com/egoist/mygo/ui"
)

//go:embed assets/providers/copilot.svg
var copilotProviderSVG []byte

//go:embed assets/providers/cursor.svg
var cursorProviderSVG []byte

//go:embed assets/providers/droid.svg
var droidProviderSVG []byte

//go:embed assets/providers/devin-color.svg
var devinProviderSVG []byte

//go:embed assets/providers/devin.svg
var devinMonoProviderSVG []byte

var (
	copilotProviderIcon = ui.MustParseSVG(copilotProviderSVG)
	cursorProviderIcon  = ui.MustParseSVG(cursorProviderSVG)
	droidProviderIcon   = ui.MustParseSVG(droidProviderSVG)
	devinProviderIcon   = ui.MustParseSVG(devinProviderSVG)
	devinMonoIcon       = ui.MustParseSVG(devinMonoProviderSVG)
)

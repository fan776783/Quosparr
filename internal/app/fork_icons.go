package app

import (
	_ "embed"
	"github.com/egoist/mygo/ui"
)

//go:embed assets/providers/copilot.svg
var copilotProviderSVG []byte

//go:embed assets/providers/cursor.svg
var cursorProviderSVG []byte

var (
	copilotProviderIcon = ui.MustParseSVG(copilotProviderSVG)
	cursorProviderIcon  = ui.MustParseSVG(cursorProviderSVG)
)

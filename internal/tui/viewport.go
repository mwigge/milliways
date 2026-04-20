package tui

import "github.com/charmbracelet/bubbles/viewport"

// NewViewport returns a configured viewport model for the block stack.
func NewViewport(width, height int) viewport.Model {
	vp := viewport.New(width, height)
	vp.SetContent("")
	return vp
}

package repl

import "context"

// Pane is an independently rendered region of the terminal surface.
// Today only REPLPane is implemented.
// Future implementations: SplitPane, LogPane, DiffPane.
type Pane interface {
	// Run starts the pane's event loop, blocking until done.
	Run(ctx context.Context) error

	// Title returns a short string for the window title / tab bar.
	Title() string
}

// REPLPane wraps the REPL, satisfying Pane.
type REPLPane struct {
	repl  *REPL
	title string
}

// NewREPLPane wraps r in a Pane. title is used for future window/tab display.
func NewREPLPane(r *REPL, title string) *REPLPane {
	if title == "" {
		title = "repl"
	}
	return &REPLPane{repl: r, title: title}
}

func (p *REPLPane) Run(ctx context.Context) error { return p.repl.Run(ctx) }
func (p *REPLPane) Title() string                 { return p.title }

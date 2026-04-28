// Copyright 2024 The milliways Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package repl

import "context"

// Pane is an independently rendered region of the terminal surface.
// Today only REPLPane is implemented (the main interactive terminal pane).
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

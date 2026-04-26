package repl

import "errors"

// ErrInputAborted is returned by Prompt when the user presses Ctrl-C.
var ErrInputAborted = errors.New("input aborted")

// InputLine is the contract between the REPL loop and the underlying line editor.
type InputLine interface {
	Prompt(prompt string) (string, error)
	AppendHistory(line string)
	SetCompleter(f func(line string) []string)
	Close() error
}

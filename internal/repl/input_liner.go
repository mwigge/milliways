package repl

import "github.com/peterh/liner"

type linerInput struct {
	state *liner.State
}

func newLinerInput() *linerInput {
	state := liner.NewLiner()
	state.SetCtrlCAborts(true)
	return &linerInput{state: state}
}

func (l *linerInput) Prompt(prompt string) (string, error) {
	s, err := l.state.Prompt(prompt)
	if err == liner.ErrPromptAborted {
		return "", ErrInputAborted
	}
	return s, err
}

func (l *linerInput) AppendHistory(line string) {
	l.state.AppendHistory(line)
}

func (l *linerInput) SetCompleter(f func(string) []string) {
	l.state.SetCompleter(f)
}

func (l *linerInput) Close() error {
	return l.state.Close()
}

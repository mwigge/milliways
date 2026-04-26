package repl

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/chzyer/readline"
)

// readlineInitMu serialises calls to readline.NewEx to avoid a data race in
// the upstream library's DefaultOnWidthChanged global-callback assignment.
var readlineInitMu sync.Mutex

// dynamicCompleter wraps a func(string)[]string so it satisfies readline.AutoCompleter.
type dynamicCompleter struct {
	mu sync.RWMutex
	fn func(string) []string
}

func (d *dynamicCompleter) set(fn func(string) []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.fn = fn
}

// Do implements readline.AutoCompleter.
func (d *dynamicCompleter) Do(line []rune, pos int) (newLine [][]rune, length int) {
	d.mu.RLock()
	fn := d.fn
	d.mu.RUnlock()

	if fn == nil {
		return nil, 0
	}

	// Complete the entire line up to cursor position.
	prefix := string(line[:pos])
	candidates := fn(prefix)
	if len(candidates) == 0 {
		return nil, 0
	}

	result := make([][]rune, 0, len(candidates))
	for _, c := range candidates {
		if len(c) >= len(prefix) {
			result = append(result, []rune(c[len(prefix):]))
		}
	}
	return result, len(prefix)
}

type readlineInput struct {
	rl        *readline.Instance
	completer *dynamicCompleter
}

func newReadlineInput(historyFile string) (*readlineInput, error) {
	if err := os.MkdirAll(filepath.Dir(historyFile), 0o750); err != nil {
		return nil, err
	}

	dc := &dynamicCompleter{}

	cfg := &readline.Config{
		Prompt:          "▶ ",
		HistoryFile:     historyFile,
		AutoComplete:    dc,
		VimMode:         true,
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
	}

	readlineInitMu.Lock()
	rl, err := readline.NewEx(cfg)
	readlineInitMu.Unlock()
	if err != nil {
		return nil, err
	}

	return &readlineInput{rl: rl, completer: dc}, nil
}

// defaultHistoryFile returns ~/.local/share/milliways/history.
func defaultHistoryFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "milliways", "history")
	}
	return filepath.Join(home, ".local", "share", "milliways", "history")
}

func (r *readlineInput) Prompt(prompt string) (string, error) {
	r.rl.SetPrompt(prompt)
	line, err := r.rl.Readline()
	if err != nil {
		if errors.Is(err, readline.ErrInterrupt) {
			return "", ErrInputAborted
		}
		if errors.Is(err, io.EOF) {
			return "", io.EOF
		}
		return "", err
	}
	return line, nil
}

func (r *readlineInput) AppendHistory(line string) {
	// chzyer/readline appends to history automatically on Readline() success.
	// No explicit append needed; this satisfies the interface.
}

func (r *readlineInput) SetCompleter(f func(string) []string) {
	r.completer.set(f)
}

func (r *readlineInput) Close() error {
	return r.rl.Close()
}

// newDefaultInput constructs the preferred InputLine (readline with vi-mode),
// falling back to liner if readline initialisation fails.
func newDefaultInput() InputLine {
	histFile := defaultHistoryFile()
	rl, err := newReadlineInput(histFile)
	if err != nil {
		slog.Warn("readline init failed, falling back to liner", "err", err)
		return newLinerInput()
	}
	return rl
}

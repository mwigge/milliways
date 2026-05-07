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

package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/term"
)

var errLineInterrupt = errors.New("line input interrupted")

type completionProvider interface {
	Complete(line string, pos int) (suffixes []string, replace int)
}

type chatLineReaderConfig struct {
	Prompt          string
	HistoryFile     string
	InterruptPrompt string
	EOFPrompt       string
	AutoComplete    completionProvider
}

type chatLineReader struct {
	in              *os.File
	out             io.Writer
	prompt          string
	historyFile     string
	interruptPrompt string
	eofPrompt       string
	completer       completionProvider
	pipeReader      *bufio.Reader

	mu      sync.Mutex
	closed  bool
	buf     []rune
	history []string
	histPos int
}

func newChatLineReader(cfg chatLineReaderConfig) (*chatLineReader, error) {
	r := &chatLineReader{
		in:              os.Stdin,
		out:             os.Stdout,
		prompt:          cfg.Prompt,
		historyFile:     cfg.HistoryFile,
		interruptPrompt: cfg.InterruptPrompt,
		eofPrompt:       cfg.EOFPrompt,
		completer:       cfg.AutoComplete,
	}
	r.loadHistory()
	r.histPos = len(r.history)
	if !term.IsTerminal(int(r.in.Fd())) {
		r.pipeReader = bufio.NewReader(r.in)
	}
	return r, nil
}

func (r *chatLineReader) SetPrompt(prompt string) {
	r.mu.Lock()
	r.prompt = prompt
	r.mu.Unlock()
}

func (r *chatLineReader) Refresh() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.redrawLocked()
}

func (r *chatLineReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	return r.saveHistory()
}

func (r *chatLineReader) Readline() (string, error) {
	if !term.IsTerminal(int(r.in.Fd())) {
		if r.pipeReader == nil {
			r.pipeReader = bufio.NewReader(r.in)
		}
		line, err := r.pipeReader.ReadString('\n')
		if err != nil {
			return strings.TrimRight(line, "\r\n"), err
		}
		line = strings.TrimRight(line, "\r\n")
		r.addHistory(line)
		return line, nil
	}

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return "", io.EOF
	}
	r.buf = nil
	r.histPos = len(r.history)
	r.redrawLocked()
	r.mu.Unlock()

	oldState, err := term.MakeRaw(int(r.in.Fd()))
	if err != nil {
		return "", err
	}
	defer func() { _ = term.Restore(int(r.in.Fd()), oldState) }()

	br := bufio.NewReader(r.in)
	for {
		ch, _, err := br.ReadRune()
		if err != nil {
			return "", err
		}
		switch ch {
		case '\r', '\n':
			r.mu.Lock()
			line := string(r.buf)
			fmt.Fprint(r.out, "\r\n")
			r.mu.Unlock()
			r.addHistory(line)
			return line, nil
		case 3:
			r.mu.Lock()
			if r.interruptPrompt != "" {
				fmt.Fprint(r.out, "\r\n"+r.interruptPrompt+"\r\n")
			} else {
				fmt.Fprint(r.out, "\r\n")
			}
			r.mu.Unlock()
			return "", errLineInterrupt
		case 4:
			r.mu.Lock()
			empty := len(r.buf) == 0
			if empty && r.eofPrompt != "" {
				fmt.Fprint(r.out, "\r\n"+r.eofPrompt+"\r\n")
			}
			r.mu.Unlock()
			if empty {
				return "", io.EOF
			}
		case 9:
			r.applyCompletion()
		case 27:
			r.handleEscape(br)
		case 8, 127:
			r.mu.Lock()
			if len(r.buf) > 0 {
				r.buf = r.buf[:len(r.buf)-1]
				r.redrawLocked()
			}
			r.mu.Unlock()
		default:
			if ch >= 32 && ch != utf8.RuneError {
				r.mu.Lock()
				r.buf = append(r.buf, ch)
				fmt.Fprint(r.out, string(ch))
				r.mu.Unlock()
			}
		}
	}
}

func (r *chatLineReader) handleEscape(br *bufio.Reader) {
	next, _, err := br.ReadRune()
	if err != nil || next != '[' {
		return
	}
	key, _, err := br.ReadRune()
	if err != nil {
		return
	}
	switch key {
	case 'A':
		r.historyMove(-1)
	case 'B':
		r.historyMove(1)
	}
}

func (r *chatLineReader) historyMove(delta int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.history) == 0 {
		return
	}
	next := r.histPos + delta
	if next < 0 {
		next = 0
	}
	if next > len(r.history) {
		next = len(r.history)
	}
	r.histPos = next
	if r.histPos == len(r.history) {
		r.buf = nil
	} else {
		r.buf = []rune(r.history[r.histPos])
	}
	r.redrawLocked()
}

func (r *chatLineReader) applyCompletion() {
	if r.completer == nil {
		return
	}
	r.mu.Lock()
	line := string(r.buf)
	r.mu.Unlock()
	suffixes, _ := r.completer.Complete(line, utf8.RuneCountInString(line))
	if len(suffixes) == 0 {
		return
	}
	if len(suffixes) == 1 {
		r.mu.Lock()
		r.buf = append(r.buf, []rune(suffixes[0])...)
		r.redrawLocked()
		r.mu.Unlock()
		return
	}
	common := commonPrefix(suffixes)
	if common != "" {
		r.mu.Lock()
		r.buf = append(r.buf, []rune(common)...)
		r.redrawLocked()
		r.mu.Unlock()
		return
	}
	r.mu.Lock()
	fmt.Fprint(r.out, "\r\n")
	for _, s := range suffixes {
		fmt.Fprintln(r.out, s)
	}
	r.redrawLocked()
	r.mu.Unlock()
}

func commonPrefix(values []string) string {
	if len(values) == 0 {
		return ""
	}
	prefix := values[0]
	for _, value := range values[1:] {
		for !strings.HasPrefix(value, prefix) && prefix != "" {
			prefix = prefix[:len(prefix)-1]
		}
	}
	return prefix
}

func (r *chatLineReader) redrawLocked() {
	fmt.Fprint(r.out, "\r\033[2K")
	fmt.Fprint(r.out, r.prompt)
	fmt.Fprint(r.out, string(r.buf))
}

func (r *chatLineReader) addHistory(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.history) == 0 || r.history[len(r.history)-1] != line {
		r.history = append(r.history, line)
	}
	r.histPos = len(r.history)
}

func (r *chatLineReader) loadHistory() {
	if r.historyFile == "" {
		return
	}
	f, err := os.Open(r.historyFile)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			r.history = append(r.history, line)
		}
	}
}

func (r *chatLineReader) saveHistory() error {
	if r.historyFile == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(r.historyFile), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(r.historyFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	start := 0
	if len(r.history) > 1000 {
		start = len(r.history) - 1000
	}
	for _, line := range r.history[start:] {
		if _, err := fmt.Fprintln(f, line); err != nil {
			return err
		}
	}
	return nil
}

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

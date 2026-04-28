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

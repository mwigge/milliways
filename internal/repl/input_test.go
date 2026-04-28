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

import (
	"testing"
)

func TestErrInputAborted_IsNotNil(t *testing.T) {
	t.Parallel()
	if ErrInputAborted == nil {
		t.Fatal("ErrInputAborted must not be nil")
	}
	if ErrInputAborted.Error() == "" {
		t.Fatal("ErrInputAborted.Error() must return a non-empty string")
	}
}

func TestLinerInput_ImplementsInputLine(t *testing.T) {
	t.Parallel()
	// compile-time interface check
	var _ InputLine = (*linerInput)(nil)
}

func TestReadlineInput_ImplementsInputLine(t *testing.T) {
	t.Parallel()
	// compile-time interface check
	var _ InputLine = (*readlineInput)(nil)
}

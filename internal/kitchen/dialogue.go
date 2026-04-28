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

package kitchen

import "strings"

const questionMarker = "?MW>"

const confirmMarker = "!MW>"

// QuestionPrefix is the stdout prefix a kitchen writes to request a free-text answer.
const QuestionPrefix = "?MW> "

// ConfirmPrefix is the stdout prefix a kitchen writes to request a y/N confirmation.
const ConfirmPrefix = "!MW> "

// IsQuestion returns true if line starts with QuestionPrefix.
func IsQuestion(line string) bool {
	return strings.HasPrefix(line, questionMarker)
}

// IsConfirm returns true if line starts with ConfirmPrefix.
func IsConfirm(line string) bool {
	return strings.HasPrefix(line, confirmMarker)
}

// StripPrefix returns the content after ?MW> or !MW> prefix,
// or the original line if neither prefix matches.
func StripPrefix(line string) string {
	if IsQuestion(line) {
		return stripDialoguePrefix(line, questionMarker, QuestionPrefix)
	}
	if IsConfirm(line) {
		return stripDialoguePrefix(line, confirmMarker, ConfirmPrefix)
	}
	return line
}

func stripDialoguePrefix(line, marker, fullPrefix string) string {
	if strings.HasPrefix(line, fullPrefix) {
		return line[len(fullPrefix):]
	}

	return line[len(marker):]
}

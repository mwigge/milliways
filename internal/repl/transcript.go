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
	"io"
	"log/slog"
	"os"
)

// ansiState tracks the ANSI escape sequence state machine used by TranscriptWriter.
type ansiState int

const (
	ansiStateNormal ansiState = iota // default: pass bytes through
	ansiStateEsc                     // consumed ESC, deciding sequence type
	ansiStateCSI                     // inside ESC [ ... sequence
	ansiStateOSC                     // inside ESC ] ... sequence (terminated by BEL or ST)
	ansiStateOSCEsc                  // inside OSC, saw ESC (looking for \ to close ST)
)

// TranscriptWriter wraps an io.Writer, passing all bytes to inner unchanged,
// while also writing ANSI-stripped bytes to logFile.
type TranscriptWriter struct {
	inner   io.Writer
	logFile *os.File
	state   ansiState
}

// NewTranscriptWriter wraps w and opens logPath for append.
// If logPath cannot be opened, returns a degraded writer that still passes
// through to w but writes no log file.
func NewTranscriptWriter(w io.Writer, logPath string) *TranscriptWriter {
	tw := &TranscriptWriter{inner: w}
	if logPath != "" {
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			slog.Warn("transcript: could not open log file", "path", logPath, "err", err)
			// Degraded mode — inner passthrough only.
		} else {
			tw.logFile = f
		}
	}
	return tw
}

// Write passes all bytes to inner unchanged and writes ANSI-stripped bytes to logFile.
// The return value reflects the number of bytes accepted from p (always len(p) on success),
// matching the io.Writer contract.
func (t *TranscriptWriter) Write(p []byte) (int, error) {
	// Pass all bytes to inner writer first.
	if _, err := t.inner.Write(p); err != nil {
		return 0, err
	}

	// If no log file, nothing left to do.
	if t.logFile == nil {
		return len(p), nil
	}

	// Strip ANSI escape sequences and write clean bytes to log.
	// We accumulate clean bytes in a local buffer to minimise syscalls.
	clean := make([]byte, 0, len(p))
	for _, b := range p {
		switch t.state {
		case ansiStateNormal:
			if b == 0x1b { // ESC
				t.state = ansiStateEsc
			} else {
				clean = append(clean, b)
			}

		case ansiStateEsc:
			switch b {
			case '[': // CSI sequence start
				t.state = ansiStateCSI
			case ']': // OSC sequence start
				t.state = ansiStateOSC
			case '7', '8': // cursor save/restore — single-byte after ESC
				t.state = ansiStateNormal
			default:
				// Any other ESC + byte: consume as a 2-char sequence, return to normal.
				t.state = ansiStateNormal
			}

		case ansiStateCSI:
			// CSI sequences end when a byte in the range 0x40–0x7E (@ through ~) is seen.
			// Intermediate bytes are in 0x20–0x2F, parameter bytes in 0x30–0x3F.
			if b >= 0x40 && b <= 0x7e {
				t.state = ansiStateNormal
			}
			// Otherwise consume the byte (parameter / intermediate).

		case ansiStateOSC:
			switch b {
			case 0x07: // BEL — terminates OSC
				t.state = ansiStateNormal
			case 0x1b: // ESC — start of String Terminator (ST = ESC \)
				t.state = ansiStateOSCEsc
			}
			// Otherwise consume the byte as part of OSC content.

		case ansiStateOSCEsc:
			// We saw ESC inside OSC; if next byte is \ it's the ST terminator.
			// Either way, return to normal (any ESC sequence inside OSC ends it).
			t.state = ansiStateNormal
		}
	}

	if len(clean) > 0 {
		if _, err := t.logFile.Write(clean); err != nil {
			slog.Warn("transcript: log write error", "err", err)
			// Don't propagate — log failure is non-fatal.
		}
	}

	return len(p), nil
}

// Close closes the underlying log file.
func (t *TranscriptWriter) Close() error {
	if t.logFile != nil {
		if err := t.logFile.Close(); err != nil {
			return err
		}
		t.logFile = nil
	}
	return nil
}

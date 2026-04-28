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

package charts

import (
	"encoding/base64"
	"strings"
	"testing"
)

// TestKittyEscape_RoundTrip verifies the kitty graphics protocol
// envelope: starts with ESC_G, ends with ESC\, the payload between
// ';' and the trailing ESC\ is valid base64 that decodes to the input.
func TestKittyEscape_RoundTrip(t *testing.T) {
	t.Parallel()
	payload := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 'X', 'Y', 'Z'}
	esc := KittyEscape(payload, 0)

	const prefix = "\x1b_G"
	const suffix = "\x1b\\"
	if !strings.HasPrefix(esc, prefix) {
		t.Fatalf("escape does not start with ESC_G: % q", esc)
	}
	if !strings.HasSuffix(esc, suffix) {
		t.Fatalf("escape does not end with ESC\\: % q", esc)
	}

	// Format: ESC _ G <key=val,...> ; <base64> ESC \
	body := strings.TrimSuffix(strings.TrimPrefix(esc, prefix), suffix)
	semi := strings.IndexByte(body, ';')
	if semi < 0 {
		t.Fatalf("escape missing ';': %q", body)
	}
	header := body[:semi]
	b64 := body[semi+1:]

	for _, want := range []string{"a=T", "f=100", "t=d", "m=0"} {
		if !strings.Contains(header, want) {
			t.Errorf("header missing %q: %q", want, header)
		}
	}
	got, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("payload mismatch: got % x want % x", got, payload)
	}
}

// TestKittyEscape_WithImageID asserts that a non-zero image ID is
// embedded in the header as `i=<id>`.
func TestKittyEscape_WithImageID(t *testing.T) {
	t.Parallel()
	esc := KittyEscape([]byte{1, 2, 3}, 7)
	if !strings.Contains(esc, "i=7") {
		t.Errorf("expected i=7 in header, got %q", esc)
	}
}

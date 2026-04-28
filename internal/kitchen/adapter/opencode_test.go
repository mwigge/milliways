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

package adapter

import (
	"context"
	"testing"
)

func TestOpenCodeAdapter_Send_WithoutPipe(t *testing.T) {
	t.Parallel()

	a := NewOpenCodeAdapter(newTestKitchen("echo"), AdapterOpts{})
	if err := a.Send(context.Background(), "msg"); err != ErrNotInteractive {
		t.Errorf("Send without pipe = %v, want ErrNotInteractive", err)
	}
}

func TestOpenCodeAdapter_Resume(t *testing.T) {
	t.Parallel()

	a := NewOpenCodeAdapter(newTestKitchen("echo"), AdapterOpts{})
	if !a.SupportsResume() {
		t.Error("SupportsResume() = false, want true")
	}
	if a.SessionID() != "" {
		t.Errorf("SessionID() = %q, want empty", a.SessionID())
	}
	caps := a.Capabilities()
	if !caps.NativeResume {
		t.Error("Capabilities.NativeResume = false, want true")
	}
	if caps.ExhaustionDetection == "" || caps.ExhaustionDetection == "none" {
		t.Error("Capabilities.ExhaustionDetection should be set")
	}
}

func TestParseGenericExhaustionText_OpenCode(t *testing.T) {
	t.Parallel()

	evt := parseGenericExhaustionText("opencode", "usage limit reached, try later", "stdout_text")
	if evt == nil {
		t.Fatal("expected exhaustion event")
	}
	if evt.RateLimit == nil || evt.RateLimit.DetectionKind != "stdout_text" {
		t.Fatalf("rate limit = %#v", evt.RateLimit)
	}
}

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

package runners

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRunDeepSeek_StreamsUsageAndCost(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	t.Setenv("DEEPSEEK_MODEL", "deepseek-v4-flash")
	t.Setenv("DEEPSEEK_TOOLS", "off")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("Authorization = %q", got)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload["model"] != "deepseek-v4-flash" {
			t.Fatalf("model = %v", payload["model"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()
	t.Setenv("DEEPSEEK_API_URL", srv.URL)

	pusher := &fakePusher{}
	obs := &mockObserver{}
	in := make(chan []byte, 1)
	in <- []byte("hi")
	close(in)

	done := make(chan struct{})
	go func() {
		RunDeepSeek(context.Background(), in, pusher, obs)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunDeepSeek did not return")
	}

	var sawData, sawChunkEnd bool
	for _, ev := range pusher.snapshot() {
		switch ev["t"] {
		case "data":
			decoded, _ := base64.StdEncoding.DecodeString(ev["b64"].(string))
			if string(decoded) == "hello" {
				sawData = true
			}
		case "chunk_end":
			sawChunkEnd = true
			if ev["input_tokens"] != 10 || ev["output_tokens"] != 5 || ev["total_tokens"] != 15 {
				t.Fatalf("chunk_end usage = %#v", ev)
			}
			if cost, _ := ev["cost_usd"].(float64); cost <= 0 {
				t.Fatalf("chunk_end cost_usd = %v, want > 0", ev["cost_usd"])
			}
		}
	}
	if !sawData || !sawChunkEnd {
		t.Fatalf("events missing data/chunk_end: %#v", pusher.snapshot())
	}
	if got := obs.counterTotal(MetricTokensIn, AgentIDDeepSeek); got != 10 {
		t.Fatalf("tokens_in = %v, want 10", got)
	}
	if got := obs.counterTotal(MetricTokensOut, AgentIDDeepSeek); got != 5 {
		t.Fatalf("tokens_out = %v, want 5", got)
	}
}

func TestRunBerget_StreamsUsageWithConfiguredModel(t *testing.T) {
	t.Setenv("BERGET_API_KEY", "test-key")
	t.Setenv("BERGET_MODEL", "gemma-4-31B-it")
	t.Setenv("BERGET_TOOLS", "off")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("Authorization = %q", got)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload["model"] != "gemma-4-31B-it" {
			t.Fatalf("model = %v", payload["model"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"berget-ok\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":3,\"total_tokens\":7}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()
	t.Setenv("BERGET_API_URL", srv.URL)

	pusher := &fakePusher{}
	obs := &mockObserver{}
	in := make(chan []byte, 1)
	in <- []byte("hi")
	close(in)

	done := make(chan struct{})
	go func() {
		RunBerget(context.Background(), in, pusher, obs)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunBerget did not return")
	}

	if events := pusher.snapshot(); len(events) == 0 || !eventsContainText(events, "berget-ok") {
		t.Fatalf("events missing berget text: %#v", events)
	}
	var sawChunkEnd bool
	for _, ev := range pusher.snapshot() {
		if ev["t"] != "chunk_end" {
			continue
		}
		sawChunkEnd = true
		if ev["input_tokens"] != 4 || ev["output_tokens"] != 3 || ev["total_tokens"] != 7 {
			t.Fatalf("chunk_end usage = %#v", ev)
		}
		if ev["cost_known"] != false {
			t.Fatalf("cost_known = %#v, want false in %#v", ev["cost_known"], ev)
		}
	}
	if !sawChunkEnd {
		t.Fatalf("events missing chunk_end: %#v", pusher.snapshot())
	}
	if got := obs.counterTotal(MetricTokensIn, AgentIDBerget); got != 4 {
		t.Fatalf("tokens_in = %v, want 4", got)
	}
	if got := obs.counterTotal(MetricTokensOut, AgentIDBerget); got != 3 {
		t.Fatalf("tokens_out = %v, want 3", got)
	}
	if got := obs.counterCount(MetricCostUSD, AgentIDBerget); got != 0 {
		t.Fatalf("cost observations = %d, want 0 for unknown provider price", got)
	}
}

func TestRunKimi_AcceptsMoonshotAPIKeyFallback(t *testing.T) {
	t.Setenv("KIMI_API_KEY", "")
	t.Setenv("MOONSHOT_API_KEY", "moonshot-key")
	t.Setenv("KIMI_MODEL", "kimi-k2.6")
	t.Setenv("KIMI_TOOLS", "off")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer moonshot-key" {
			t.Fatalf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":1,\"total_tokens\":3}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()
	t.Setenv("KIMI_API_URL", srv.URL)

	pusher := &fakePusher{}
	in := make(chan []byte, 1)
	in <- []byte("ping")
	close(in)

	done := make(chan struct{})
	go func() {
		RunKimi(context.Background(), in, pusher, &mockObserver{})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunKimi did not return")
	}
	if events := pusher.snapshot(); len(events) == 0 || !eventsContainText(events, "ok") {
		t.Fatalf("events missing kimi text: %#v", events)
	}
}

func TestRunKimi_MarksCostUnknownWithoutConfiguredRates(t *testing.T) {
	t.Setenv("KIMI_API_KEY", "test-key")
	t.Setenv("KIMI_MODEL", "kimi-k2.6")
	t.Setenv("KIMI_TOOLS", "off")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()
	t.Setenv("KIMI_API_URL", srv.URL)

	pusher := &fakePusher{}
	obs := &mockObserver{}
	in := make(chan []byte, 1)
	in <- []byte("ping")
	close(in)

	done := make(chan struct{})
	go func() {
		RunKimi(context.Background(), in, pusher, obs)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunKimi did not return")
	}

	var sawChunkEnd bool
	for _, ev := range pusher.snapshot() {
		if ev["t"] != "chunk_end" {
			continue
		}
		sawChunkEnd = true
		if ev["cost_known"] != false {
			t.Fatalf("cost_known = %#v, want false in %#v", ev["cost_known"], ev)
		}
		if _, ok := ev["cost_usd"]; ok {
			t.Fatalf("cost_usd should be omitted when provider price is unknown: %#v", ev)
		}
	}
	if !sawChunkEnd {
		t.Fatalf("events missing chunk_end: %#v", pusher.snapshot())
	}
	if got := obs.counterTotal(MetricTokensIn, AgentIDKimi); got != 10 {
		t.Fatalf("tokens_in = %v, want 10", got)
	}
	if got := obs.counterTotal(MetricTokensOut, AgentIDKimi); got != 5 {
		t.Fatalf("tokens_out = %v, want 5", got)
	}
	if got := obs.counterCount(MetricCostUSD, AgentIDKimi); got != 0 {
		t.Fatalf("cost observations = %d, want 0 for unknown provider price", got)
	}
}

func TestRunKimi_SanitizesMalformedURLSecrets(t *testing.T) {
	t.Setenv("KIMI_API_KEY", "test-key")
	t.Setenv("KIMI_API_URL", "https://example.test/v1/chat/completions?api_key=secret-token\nbad")
	t.Setenv("KIMI_TOOLS", "off")

	pusher := &fakePusher{}
	in := make(chan []byte, 1)
	in <- []byte("ping")
	close(in)

	done := make(chan struct{})
	go func() {
		RunKimi(context.Background(), in, pusher, &mockObserver{})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunKimi did not return")
	}

	for _, ev := range pusher.snapshot() {
		if ev["t"] != "err" {
			continue
		}
		msg, _ := ev["msg"].(string)
		if strings.Contains(msg, "secret-token") || strings.Contains(msg, "api_key=secret") {
			t.Fatalf("error leaked malformed URL secret: %q", msg)
		}
		if !strings.Contains(msg, "[REDACTED]") && !strings.Contains(msg, "[invalid-url]") {
			t.Fatalf("error did not show sanitized URL/error marker: %q", msg)
		}
		return
	}
	t.Fatalf("events missing err: %#v", pusher.snapshot())
}

func eventsContainText(events []map[string]any, want string) bool {
	for _, ev := range events {
		if ev["t"] != "data" {
			continue
		}
		raw, _ := ev["b64"].(string)
		decoded, _ := base64.StdEncoding.DecodeString(raw)
		if strings.Contains(string(decoded), want) {
			return true
		}
	}
	return false
}

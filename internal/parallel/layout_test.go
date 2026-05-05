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

package parallel

import (
	"strings"
	"testing"
)

func TestFormatTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input int
		want  string
	}{
		{"zero", 0, "0"},
		{"under 1000", 999, "999"},
		{"exact 1000", 1000, "1.0k"},
		{"1234", 1234, "1.2k"},
		{"12400", 12400, "12.4k"},
		{"100000", 100000, "100.0k"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := formatTokens(tt.input)
			if got != tt.want {
				t.Errorf("formatTokens(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestRenderHeader_ThreeSlots(t *testing.T) {
	t.Parallel()

	slots := []SlotRecord{
		{SlotN: 1, Provider: "claude", Status: "running", TokensOut: 12400},
		{SlotN: 2, Provider: "codex", Status: "running", TokensOut: 8100},
		{SlotN: 3, Provider: "local", Status: "idle", TokensOut: 3200},
	}
	quotas := map[string]QuotaSummary{
		"claude": {UsedToday: 34, LimitDay: 100},
		"codex":  {UsedToday: 12, LimitDay: 100},
		"local":  {UsedToday: 5, LimitDay: 100},
	}
	totalTokens := 12400 + 8100 + 3200

	result := RenderHeader(slots, quotas, totalTokens, 120)

	// Each provider name should appear
	for _, want := range []string{"claude", "codex", "local"} {
		if !strings.Contains(result, want) {
			t.Errorf("RenderHeader() missing provider %q; got:\n%s", want, result)
		}
	}
	// Total tokens should appear
	if !strings.Contains(result, "23.7k") {
		t.Errorf("RenderHeader() missing total token count; got:\n%s", result)
	}
	// Quota percentages should appear
	if !strings.Contains(result, "34%") {
		t.Errorf("RenderHeader() missing quota 34%%; got:\n%s", result)
	}
	// Running slots get green bullet
	if !strings.Contains(result, "\033[32m●\033[0m") {
		t.Errorf("RenderHeader() missing green bullet for running slot; got:\n%s", result)
	}
}

func TestRenderHeader_QuotaHighYellow(t *testing.T) {
	t.Parallel()

	slots := []SlotRecord{
		{SlotN: 1, Provider: "claude", Status: "running", TokensOut: 100},
	}
	quotas := map[string]QuotaSummary{
		"claude": {UsedToday: 85, LimitDay: 100},
	}

	result := RenderHeader(slots, quotas, 100, 120)

	// >80% quota => yellow color on percentage
	if !strings.Contains(result, "\033[33m") {
		t.Errorf("RenderHeader() missing yellow color for 85%% quota; got:\n%s", result)
	}
}

func TestRenderHeader_QuotaCriticalRed(t *testing.T) {
	t.Parallel()

	slots := []SlotRecord{
		{SlotN: 1, Provider: "claude", Status: "running", TokensOut: 100},
	}
	quotas := map[string]QuotaSummary{
		"claude": {UsedToday: 97, LimitDay: 100},
	}

	result := RenderHeader(slots, quotas, 100, 120)

	// >95% quota => red color on percentage
	if !strings.Contains(result, "\033[31m") {
		t.Errorf("RenderHeader() missing red color for 97%% quota; got:\n%s", result)
	}
}

func TestRenderHeader_NarrowTerminal(t *testing.T) {
	t.Parallel()

	slots := []SlotRecord{
		{SlotN: 1, Provider: "claude", Status: "running", TokensOut: 5000},
		{SlotN: 2, Provider: "codex", Status: "running", TokensOut: 5000},
	}

	// Narrow terminal: termWidth < 60
	result := RenderHeader(slots, nil, 10000, 50)

	// Should collapse to single-line format
	if !strings.Contains(result, "parallel group") {
		t.Errorf("RenderHeader() narrow: missing 'parallel group'; got:\n%s", result)
	}
	if !strings.Contains(result, "running") {
		t.Errorf("RenderHeader() narrow: missing 'running'; got:\n%s", result)
	}
}

func TestRenderHeader_NarrowBySlotRatio(t *testing.T) {
	t.Parallel()

	// 5 slots with termWidth=80 => 80/5 = 16 < 20 => collapse
	slots := make([]SlotRecord, 5)
	for i := range slots {
		slots[i] = SlotRecord{SlotN: i + 1, Provider: "claude", Status: "running"}
	}

	result := RenderHeader(slots, nil, 0, 80)

	if !strings.Contains(result, "parallel group") {
		t.Errorf("RenderHeader() slot-ratio narrow: missing 'parallel group'; got:\n%s", result)
	}
}

func TestRenderHeader_NoQuota(t *testing.T) {
	t.Parallel()

	slots := []SlotRecord{
		{SlotN: 1, Provider: "claude", Status: "running", TokensOut: 1000},
	}

	result := RenderHeader(slots, nil, 1000, 120)

	if !strings.Contains(result, "— quota") {
		t.Errorf("RenderHeader() no quota: missing '— quota'; got:\n%s", result)
	}
}

func TestRenderHeader_DoneSlotYellowBullet(t *testing.T) {
	t.Parallel()

	slots := []SlotRecord{
		{SlotN: 1, Provider: "claude", Status: "done", TokensOut: 100},
	}

	result := RenderHeader(slots, nil, 100, 120)

	// done/idle => yellow bullet
	if !strings.Contains(result, "\033[33m●\033[0m") {
		t.Errorf("RenderHeader() done slot: missing yellow bullet; got:\n%s", result)
	}
}

func TestRenderHeader_ErrorSlotRedBullet(t *testing.T) {
	t.Parallel()

	slots := []SlotRecord{
		{SlotN: 1, Provider: "claude", Status: "error", TokensOut: 100},
	}

	result := RenderHeader(slots, nil, 100, 120)

	// error => red bullet
	if !strings.Contains(result, "\033[31m●\033[0m") {
		t.Errorf("RenderHeader() error slot: missing red bullet; got:\n%s", result)
	}
}

func TestLaunch_HeadlessFallback(t *testing.T) {
	// t.Setenv modifies env; cannot run parallel.
	t.Setenv("TERM_PROGRAM", "")

	var buf strings.Builder
	result := DispatchResult{
		GroupID: "grp-test1234",
		Slots: []SlotRecord{
			{SlotN: 1, Handle: 101, Provider: "claude"},
			{SlotN: 2, Handle: 102, Provider: "codex"},
		},
	}

	err := launchWithWriter(&buf, result, "grp-test1234")
	if err != nil {
		t.Fatalf("Launch() headless: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "WezTerm not detected") {
		t.Errorf("Launch() headless: missing WezTerm message; got:\n%s", output)
	}
	if !strings.Contains(output, "101") {
		t.Errorf("Launch() headless: missing handle 101; got:\n%s", output)
	}
	if !strings.Contains(output, "102") {
		t.Errorf("Launch() headless: missing handle 102; got:\n%s", output)
	}
	if !strings.Contains(output, "claude") {
		t.Errorf("Launch() headless: missing provider claude; got:\n%s", output)
	}
}

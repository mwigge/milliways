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
	"strings"
	"testing"
)

func TestFormatUsageInlineShowsCostPairAndTotal(t *testing.T) {
	got := formatUsageInline(usageStats{
		InputTokens:  1683,
		OutputTokens: 115,
		CostUSD:      0.0006,
	})
	for _, want := range []string{"$0.0006", "in 1.7k", "out 115", "total 1.8k tok"} {
		if !strings.Contains(got, want) {
			t.Fatalf("usage inline missing %q: %q", want, got)
		}
	}
}

func TestFormatCostUsesStableZeroDisplay(t *testing.T) {
	if got := formatCost(0); got != "$0.00" {
		t.Fatalf("zero cost = %q, want $0.00", got)
	}
	if got := formatCost(0.0006); got != "$0.0006" {
		t.Fatalf("small cost = %q, want $0.0006", got)
	}
}

func TestFormatUsageCompactShowsTotalAndCost(t *testing.T) {
	got := formatUsageCompact(usageStats{TotalTokens: 31_000, CostUSD: 0.87})
	if got != "31.0k tok $0.87" {
		t.Fatalf("usage compact = %q, want %q", got, "31.0k tok $0.87")
	}
}

func TestFormatUsageTotalLabelUsesSameTokenUnits(t *testing.T) {
	if got := formatUsageTotalLabel(1_250_000); got != "1.2m tok" {
		t.Fatalf("usage total = %q, want 1.2m tok", got)
	}
}

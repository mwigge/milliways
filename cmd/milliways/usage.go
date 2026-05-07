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

import "fmt"

type usageStats struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	CostUSD      float64
}

func (u usageStats) total() int {
	if u.TotalTokens > 0 {
		return u.TotalTokens
	}
	return u.InputTokens + u.OutputTokens
}

func (u usageStats) hasTokens() bool {
	return u.InputTokens > 0 || u.OutputTokens > 0 || u.TotalTokens > 0
}

func (u usageStats) hasCost() bool {
	return u.CostUSD > 0
}

func (u usageStats) isZero() bool {
	return !u.hasTokens() && !u.hasCost()
}

// formatCost renders a USD cost with appropriate precision.
func formatCost(usd float64) string {
	switch {
	case usd <= 0:
		return "$0.00"
	case usd < 0.01:
		return fmt.Sprintf("$%.4f", usd)
	case usd < 10:
		return fmt.Sprintf("$%.2f", usd)
	default:
		return fmt.Sprintf("$%.1f", usd)
	}
}

// formatCostVerbose always shows 4 decimals, for logs/audit.
func formatCostVerbose(usd float64) string {
	return fmt.Sprintf("$%.4f", usd)
}

// formatTokenCount renders token counts with compact lower-case suffixes.
func formatTokenCount(n int) string {
	switch {
	case n < 0:
		return "0"
	case n < 1000:
		return fmt.Sprintf("%d", n)
	case n < 1_000_000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return fmt.Sprintf("%.1fm", float64(n)/1_000_000)
	}
}

func formatUsageTotalLabel(totalTokens int) string {
	return formatTokenCount(totalTokens) + " tok"
}

func formatUsagePair(in, out int) string {
	return fmt.Sprintf("in %s / out %s", formatTokenCount(in), formatTokenCount(out))
}

func formatUsageInline(u usageStats) string {
	var parts []string
	if u.hasCost() {
		parts = append(parts, formatCost(u.CostUSD))
	}
	if u.hasTokens() {
		pair := formatUsagePair(u.InputTokens, u.OutputTokens)
		if total := u.total(); total > 0 {
			pair += " / total " + formatTokenCount(total) + " tok"
		} else {
			pair += " tok"
		}
		parts = append(parts, pair)
	}
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return parts[0] + " · " + parts[1]
}

func formatUsageCompact(u usageStats) string {
	var out string
	if u.hasTokens() {
		out = formatUsageTotalLabel(u.total())
	}
	if u.hasCost() {
		if out == "" {
			return formatCost(u.CostUSD)
		}
		return out + " " + formatCost(u.CostUSD)
	}
	return out
}

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
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/mwigge/milliways/internal/daemon/metrics"
	"github.com/mwigge/milliways/internal/rpc"
)

var dashboardAgents = []string{"claude", "codex", "copilot", "gemini", "pool", "minimax", "local"}

type dashWindow struct {
	label string
	tier  string
	from  string
}

var dashWindows = []dashWindow{
	{"1 min", "raw", "-1min"},
	{"1 hour", "raw", "-1h"},
	{"24 h", "hourly", "-24h"},
	{"7 d", "daily", "-7d"},
	{"30 d", "daily", "-30d"},
}

const numWindows = 5 // must match len(dashWindows)

type cellData struct {
	tokIn  float64
	tokOut float64
	cost   float64
	errors float64
}

type agentRow struct {
	cells [numWindows]cellData
}

// runMetricsDashboard prints a live rolling metrics table.
// watch=true refreshes every 5 seconds until Ctrl+C.
func runMetricsDashboard(socket string, watch bool) {
	for {
		rows := fetchDashboard(socket)
		if watch {
			clearTerminal()
		}
		printDashboard(rows)
		if !watch {
			return
		}
		fmt.Fprintf(os.Stderr, "\n  refreshing every 5s — Ctrl+C to exit\n")
		time.Sleep(5 * time.Second)
	}
}

func fetchDashboard(socket string) map[string]agentRow {
	c, err := rpc.Dial(socket)
	if err != nil {
		die("dial %s: %v", socket, err)
	}
	defer c.Close()

	result := make(map[string]agentRow)

	for wi, win := range dashWindows {
		for _, metricName := range []string{"tokens_in", "tokens_out", "cost_usd", "error_count"} {
			for _, agent := range dashboardAgents {
				agentCopy := agent
				p := metrics.RollupGetParams{
					Metric:  metricName,
					Tier:    win.tier,
					Range:   &metrics.Range{From: win.from},
					AgentID: &agentCopy,
				}
				var res metrics.RollupGetResult
				if err := c.Call("metrics.rollup.get", p, &res); err != nil {
					continue
				}
				var sum float64
				for _, b := range res.Buckets {
					sum += b.Sum
				}
				if sum == 0 {
					continue
				}
				row := result[agent]
				switch metricName {
				case "tokens_in":
					row.cells[wi].tokIn = sum
				case "tokens_out":
					row.cells[wi].tokOut = sum
				case "cost_usd":
					row.cells[wi].cost = sum
				case "error_count":
					row.cells[wi].errors = sum
				}
				result[agent] = row
			}
		}
	}
	return result
}

func printDashboard(data map[string]agentRow) {
	now := time.Now().Format("15:04:05")
	fmt.Printf("\nmilliways metrics  %s\n\n", now)

	hdr := fmt.Sprintf("%-10s │", "runner")
	for _, w := range dashWindows {
		hdr += fmt.Sprintf("  %-22s", w.label)
	}
	fmt.Println(hdr)
	sep := strings.Repeat("─", 10) + "┼" + strings.Repeat("─", numWindows*24)
	fmt.Println(sep)

	var totals [numWindows]cellData
	for _, agent := range dashboardAgents {
		row := data[agent]
		line := fmt.Sprintf("%-10s │", agent)
		for wi := range dashWindows {
			cell := row.cells[wi]
			totals[wi].tokIn += cell.tokIn
			totals[wi].tokOut += cell.tokOut
			totals[wi].cost += cell.cost
			totals[wi].errors += cell.errors
			line += "  " + formatCell(cell)
		}
		fmt.Println(line)
	}

	fmt.Println(sep)
	totLine := fmt.Sprintf("%-10s │", "total")
	for _, cell := range totals {
		totLine += "  " + formatCell(cell)
	}
	fmt.Println(totLine)
	fmt.Println()
	fmt.Println("  columns: tok_in/tok_out  $cost  errors   (— = no activity)")
}

func formatCell(c cellData) string {
	if c.tokIn == 0 && c.tokOut == 0 && c.cost == 0 && c.errors == 0 {
		return fmt.Sprintf("%-22s", "—")
	}
	s := fmt.Sprintf("%s/%s", fmtTokens(c.tokIn), fmtTokens(c.tokOut))
	if c.cost > 0 {
		s += fmt.Sprintf(" $%.3f", c.cost)
	}
	if c.errors > 0 {
		s += fmt.Sprintf(" ✗%d", int(math.Round(c.errors)))
	}
	if len(s) > 22 {
		s = s[:21] + "…"
	}
	return fmt.Sprintf("%-22s", s)
}

func fmtTokens(t float64) string {
	if t == 0 {
		return "0"
	}
	if t >= 1_000_000 {
		return fmt.Sprintf("%.1fM", t/1_000_000)
	}
	if t >= 1_000 {
		return fmt.Sprintf("%.1fk", t/1_000)
	}
	return fmt.Sprintf("%.0f", t)
}

func clearTerminal() {
	fmt.Print("\033[H\033[2J")
}

// callMetricsRollup is used by the single-metric milliwaysctl metrics path.
func callMetricsRollup(socket, metricName, tier, fromRange, agentID string) {
	p := map[string]any{
		"metric": metricName,
		"tier":   tier,
	}
	if fromRange != "" {
		p["range"] = map[string]any{"from": fromRange}
	}
	if agentID != "" {
		p["agent_id"] = agentID
	}
	c, err := rpc.Dial(socket)
	if err != nil {
		die("dial %s: %v", socket, err)
	}
	defer c.Close()
	var res any
	if err := c.Call("metrics.rollup.get", p, &res); err != nil {
		die("metrics.rollup.get: %v", err)
	}
	out, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		die("encode response: %v", err)
	}
	fmt.Println(string(out))
}

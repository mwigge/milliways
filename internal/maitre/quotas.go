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

package maitre

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/mwigge/milliways/internal/pantry"
)

// QuotaConfig defines resource limits per kitchen.
type QuotaConfig struct {
	MaxConcurrent   int `yaml:"max_concurrent"`
	MaxDurationMin  int `yaml:"max_duration_min"`
	MaxMemoryMB     int `yaml:"max_memory_mb"`
	DailyDispatches int `yaml:"daily_dispatches"`
	CooldownSec     int `yaml:"cooldown_sec"`
}

// GlobalQuotaConfig defines system-wide limits.
type GlobalQuotaConfig struct {
	MaxTotalConcurrent int `yaml:"max_total_concurrent"`
	PauseIfMemoryAbove int `yaml:"pause_if_memory_above"` // percentage
}

// QuotaCheck validates whether a dispatch is allowed under quota constraints.
type QuotaCheck struct {
	pdb          *pantry.DB
	kitchenQuota map[string]QuotaConfig
	globalQuota  GlobalQuotaConfig
}

// NewQuotaCheck creates a quota checker.
func NewQuotaCheck(pdb *pantry.DB, kitchenQuota map[string]QuotaConfig, globalQuota GlobalQuotaConfig) *QuotaCheck {
	return &QuotaCheck{
		pdb:          pdb,
		kitchenQuota: kitchenQuota,
		globalQuota:  globalQuota,
	}
}

// QuotaResult indicates whether dispatch is allowed.
type QuotaResult struct {
	Allowed bool
	Reason  string
}

// Check evaluates all quota constraints for a kitchen dispatch.
func (qc *QuotaCheck) Check(kitchenName string) QuotaResult {
	// Check per-kitchen daily limit
	if quota, ok := qc.kitchenQuota[kitchenName]; ok && quota.DailyDispatches > 0 {
		daily, err := qc.pdb.Quotas().DailyDispatches(kitchenName)
		if err == nil && daily >= quota.DailyDispatches {
			return QuotaResult{
				Allowed: false,
				Reason:  fmt.Sprintf("%s: daily limit reached (%d/%d)", kitchenName, daily, quota.DailyDispatches),
			}
		}
	}

	// Check per-kitchen concurrent limit
	if quota, ok := qc.kitchenQuota[kitchenName]; ok && quota.MaxConcurrent > 0 {
		running, err := countRunningTickets(qc.pdb, kitchenName)
		if err == nil && running >= quota.MaxConcurrent {
			return QuotaResult{
				Allowed: false,
				Reason:  fmt.Sprintf("%s: concurrent limit reached (%d/%d)", kitchenName, running, quota.MaxConcurrent),
			}
		}
	}

	// Check global concurrent limit
	if qc.globalQuota.MaxTotalConcurrent > 0 {
		running, err := countRunningTickets(qc.pdb, "")
		if err == nil && running >= qc.globalQuota.MaxTotalConcurrent {
			return QuotaResult{
				Allowed: false,
				Reason:  fmt.Sprintf("global: concurrent limit reached (%d/%d)", running, qc.globalQuota.MaxTotalConcurrent),
			}
		}
	}

	// Check system memory (macOS/Linux)
	if qc.globalQuota.PauseIfMemoryAbove > 0 {
		memPct := systemMemoryPercent()
		if memPct > 0 && memPct >= qc.globalQuota.PauseIfMemoryAbove {
			return QuotaResult{
				Allowed: false,
				Reason:  fmt.Sprintf("system memory at %d%% (threshold: %d%%)", memPct, qc.globalQuota.PauseIfMemoryAbove),
			}
		}
	}

	return QuotaResult{Allowed: true}
}

func countRunningTickets(pdb *pantry.DB, kitchenFilter string) (int, error) {
	tickets, err := pdb.Tickets().List("running")
	if err != nil {
		return 0, err
	}
	count := 0
	for _, t := range tickets {
		if kitchenFilter == "" || t.Kitchen == kitchenFilter {
			count++
		}
	}
	return count, nil
}

// systemMemoryPercent returns the percentage of system memory in use.
// Returns 0 if unable to determine (non-fatal).
func systemMemoryPercent() int {
	switch runtime.GOOS {
	case "darwin":
		return darwinMemoryPercent()
	case "linux":
		return linuxMemoryPercent()
	default:
		return 0
	}
}

func linuxMemoryPercent() int {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	var memTotal uint64
	var memAvailable uint64
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		switch key {
		case "MemTotal":
			if v, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
				memTotal = v * 1024 // kB -> bytes
			}
		case "MemAvailable":
			if v, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
				memAvailable = v * 1024
			}
		}
	}
	if memTotal == 0 {
		return 0
	}
	used := memTotal
	if memAvailable > 0 {
		used = memTotal - memAvailable
	}
	return int(used * 100 / memTotal)
}

func darwinMemoryPercent() int {
	// Get total memory via sysctl
	totalOut, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
	if err != nil {
		return 0
	}
	total, err := strconv.ParseUint(strings.TrimSpace(string(totalOut)), 10, 64)
	if err != nil || total == 0 {
		return 0
	}

	// Get page size and free pages via vm_stat
	vmOut, err := exec.Command("vm_stat").Output()
	if err != nil {
		return 0
	}

	pageSize := uint64(16384) // default Apple Silicon page size
	freePages := uint64(0)
	for _, line := range strings.Split(string(vmOut), "\n") {
		if strings.Contains(line, "page size of") {
			parts := strings.Fields(line)
			for i, p := range parts {
				if p == "of" && i+1 < len(parts) {
					if ps, err := strconv.ParseUint(parts[i+1], 10, 64); err == nil {
						pageSize = ps
					}
				}
			}
		}
		if strings.HasPrefix(line, "Pages free:") {
			freePages = parseVMStatValue(line)
		}
	}

	free := freePages * pageSize
	if total <= free {
		return 0
	}
	used := total - free
	return int(used * 100 / total)
}

func parseVMStatValue(line string) uint64 {
	parts := strings.Split(line, ":")
	if len(parts) != 2 {
		return 0
	}
	s := strings.TrimSpace(parts[1])
	s = strings.TrimSuffix(s, ".")
	v, _ := strconv.ParseUint(s, 10, 64)
	return v
}

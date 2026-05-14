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

package security

import "time"

// Mode controls how MilliWays reacts to security findings.
type Mode string

const (
	ModeOff     Mode = "off"
	ModeObserve Mode = "observe"
	ModeWarn    Mode = "warn"
	ModeStrict  Mode = "strict"
	ModeCI      Mode = "ci"
)

// NormalizeMode returns m when it is recognized, otherwise the recommended
// interactive default.
func NormalizeMode(m Mode) Mode {
	switch m {
	case ModeOff, ModeObserve, ModeWarn, ModeStrict, ModeCI:
		return m
	default:
		return ModeWarn
	}
}

// ScanKind identifies the security scan layer that produced a run.
type ScanKind string

const (
	ScanStartup       ScanKind = "startup"
	ScanDependency    ScanKind = "dependency"
	ScanSecret        ScanKind = "secret"
	ScanSAST          ScanKind = "sast"
	ScanIOC           ScanKind = "ioc"
	ScanClientProfile ScanKind = "client-profile"
	ScanCommand       ScanKind = "command"
)

// Posture is the aggregated security state for a workspace.
type Posture string

const (
	PostureUnknown Posture = "unknown"
	PostureOK      Posture = "ok"
	PostureWarn    Posture = "warn"
	PostureBlock   Posture = "block"
)

// ScanRun records durable metadata for one security scan attempt.
type ScanRun struct {
	ID            int64
	Kind          ScanKind
	Workspace     string
	Status        string
	StartedAt     time.Time
	CompletedAt   time.Time
	ToolName      string
	ToolVersion   string
	FindingsTotal int
	WarnCount     int
	BlockCount    int
	Error         string
}

// Warning records non-CVE security posture issues such as missing scanners,
// unsafe client profiles, IOC matches, or command firewall blocks.
type Warning struct {
	ID           int64
	Workspace    string
	Category     FindingCategory
	Severity     string
	Source       string
	Message      string
	Status       FindingStatus
	ScanRunID    int64
	FirstSeen    time.Time
	LastSeen     time.Time
	ResolvedAt   time.Time
	EvidenceHash string
	Remediation  string
}

// ClientProfileState summarizes the currently cached posture for one client.
type ClientProfileState struct {
	Client     string
	ConfigHash string
	Posture    Posture
	CheckedAt  time.Time
}

// Status is the single summary model rendered by CLI, terminal, and future RPC
// surfaces. Counts maps are keyed by category/severity string values.
type Status struct {
	Workspace            string
	Mode                 Mode
	Posture              Posture
	GeneratedAt          time.Time
	LastStartupScan      *ScanRun
	LastDependencyScan   *ScanRun
	CountsByCategory     map[FindingCategory]int
	CountsBySeverity     map[string]int
	ActiveClientProfiles []ClientProfileState
	Warnings             []Warning
}

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

// Finding is one CVE finding from an OSV scan.
type Finding struct {
	CVEID            string
	PackageName      string
	InstalledVersion string
	FixedInVersion   string // empty if no fix available
	Severity         string // CRITICAL|HIGH|MEDIUM|LOW
	Ecosystem        string
	Summary          string
	ScanSource       string // lockfile path that triggered this finding
}

// ScanResult is the output of one scan run.
type ScanResult struct {
	Findings  []Finding
	ScannedAt time.Time
	LockFiles []string // which lockfiles were scanned
}

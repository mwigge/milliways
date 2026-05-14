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

// scanner.go calls the osv-scanner CLI as a subprocess — the same pattern used
// for all other external tools in milliways (claude, codex, copilot, gemini).
// The binary is discovered via PATH; if absent scanning is skipped gracefully.
// Install via: milliwaysctl security install-scanner
//              or: go install github.com/google/osv-scanner/v2/cmd/osv-scanner@latest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"
)

// ErrNoLockfiles is returned when Scan is called with an empty lockfiles list.
var ErrNoLockfiles = errors.New("no lockfiles provided")

// ErrScannerNotFound is returned when the osv-scanner binary is not on PATH.
var ErrScannerNotFound = errors.New("osv-scanner not found on PATH; run: milliwaysctl security install-scanner")

// SupportedLockfiles is the set of filenames DiscoverLockfiles matches.
var SupportedLockfiles = []string{
	"go.sum",
	"Cargo.lock",
	"pnpm-lock.yaml",
	"package-lock.json",
	"requirements.txt",
	"pdm.lock",
}

const (
	defaultLockfileDiscoveryMaxDepth = 6
	defaultLockfileDiscoveryMaxFiles = 8192
)

// LockfileDiscoveryOptions controls recursive lockfile discovery.
type LockfileDiscoveryOptions struct {
	// MaxDepth is the maximum directory depth below root. A zero value uses the
	// default; a negative value disables the depth limit.
	MaxDepth int
	// MaxFiles is the maximum number of regular files inspected. A zero value
	// uses the default; a negative value disables the file-count limit.
	MaxFiles int
	// IgnoreDirs names directories that should not be walked.
	IgnoreDirs []string
}

var defaultLockfileIgnoreDirs = []string{
	".git",
	".hg",
	".svn",
	".milliways",
	"node_modules",
	"vendor",
	"target",
	"dist",
	"build",
	".next",
	".turbo",
	".cache",
}

// DiscoverLockfiles walks root recursively and returns paths of files whose
// basename is in SupportedLockfiles. Discovery is bounded by default so large
// dependency trees do not dominate security startup work.
func DiscoverLockfiles(root string) []string {
	return DiscoverLockfilesWithOptions(root, LockfileDiscoveryOptions{})
}

// DiscoverLockfilesWithOptions walks root recursively with explicit bounds.
func DiscoverLockfilesWithOptions(root string, opts LockfileDiscoveryOptions) []string {
	if root == "" {
		return nil
	}
	maxDepth := opts.MaxDepth
	if maxDepth == 0 {
		maxDepth = defaultLockfileDiscoveryMaxDepth
	}
	maxFiles := opts.MaxFiles
	if maxFiles == 0 {
		maxFiles = defaultLockfileDiscoveryMaxFiles
	}
	ignoreDirs := make(map[string]struct{}, len(defaultLockfileIgnoreDirs)+len(opts.IgnoreDirs))
	for _, name := range defaultLockfileIgnoreDirs {
		ignoreDirs[name] = struct{}{}
	}
	for _, name := range opts.IgnoreDirs {
		ignoreDirs[name] = struct{}{}
	}
	supported := make(map[string]struct{}, len(SupportedLockfiles))
	for _, name := range SupportedLockfiles {
		supported[name] = struct{}{}
	}

	var found []string
	visitedFiles := 0
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if path == root {
			return nil
		}
		if d.IsDir() {
			if _, ignored := ignoreDirs[d.Name()]; ignored {
				return filepath.SkipDir
			}
			if maxDepth >= 0 && depthBelow(root, path) > maxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		visitedFiles++
		if maxFiles >= 0 && visitedFiles > maxFiles {
			return filepath.SkipAll
		}
		if _, ok := supported[d.Name()]; !ok {
			return nil
		}
		found = append(found, path)
		return nil
	})
	sort.Strings(found)
	return found
}

func depthBelow(root, path string) int {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return 0
	}
	depth := 1
	for _, r := range rel {
		if os.IsPathSeparator(uint8(r)) {
			depth++
		}
	}
	return depth
}

// ScannerPath returns the path to the osv-scanner binary, or empty string.
func ScannerPath() string {
	p, _ := exec.LookPath("osv-scanner")
	return p
}

// osvOutput is the subset of osv-scanner --format json output we parse.
type osvOutput struct {
	Results []struct {
		Source struct {
			Path string `json:"path"`
		} `json:"source"`
		Packages []struct {
			Package struct {
				Name      string `json:"name"`
				Version   string `json:"version"`
				Ecosystem string `json:"ecosystem"`
			} `json:"package"`
			Vulnerabilities []struct {
				ID       string   `json:"id"`
				Aliases  []string `json:"aliases"`
				Summary  string   `json:"summary"`
				Affected []struct {
					Ranges []struct {
						Events []struct {
							Fixed string `json:"fixed,omitempty"`
						} `json:"events"`
					} `json:"ranges"`
				} `json:"affected"`
			} `json:"vulnerabilities"`
			Groups []struct {
				IDs         []string `json:"ids"`
				MaxSeverity string   `json:"max_severity"`
			} `json:"groups"`
		} `json:"packages"`
	} `json:"results"`
}

// Scan runs osv-scanner on the given lockfiles and maps the output to Findings.
// Returns ErrNoLockfiles when lockfiles is empty, ErrScannerNotFound when the
// binary is absent. Treats exit code 1 (vulnerabilities found) as success.
func Scan(ctx context.Context, lockfiles []string) (ScanResult, error) {
	if len(lockfiles) == 0 {
		return ScanResult{}, ErrNoLockfiles
	}
	bin := ScannerPath()
	if bin == "" {
		return ScanResult{}, ErrScannerNotFound
	}

	args := []string{"--format", "json"}
	for _, lf := range lockfiles {
		args = append(args, "--lockfile", lf)
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.Output()

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			switch exitErr.ExitCode() {
			case 1:
				// exit 1 = vulnerabilities found — parse normally
			case 128:
				// exit 128 = no packages found — return empty
				return ScanResult{ScannedAt: time.Now().UTC(), LockFiles: lockfiles}, nil
			default:
				return ScanResult{}, fmt.Errorf("osv-scanner exit %d: %s", exitErr.ExitCode(), exitErr.Stderr)
			}
		} else {
			return ScanResult{}, fmt.Errorf("osv-scanner: %w", err)
		}
	}

	if len(out) == 0 {
		return ScanResult{ScannedAt: time.Now().UTC(), LockFiles: lockfiles}, nil
	}

	var parsed osvOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return ScanResult{}, fmt.Errorf("parse osv-scanner output: %w", err)
	}

	var findings []Finding
	for _, result := range parsed.Results {
		src := result.Source.Path
		for _, pkg := range result.Packages {
			for _, grp := range pkg.Groups {
				cveID := firstCVE(grp.IDs)
				if cveID == "" {
					continue
				}
				summary, fixedIn := "", ""
				for _, v := range pkg.Vulnerabilities {
					if idInSlice(v.ID, grp.IDs) || anyAliasInSlice(v.Aliases, grp.IDs) {
						summary = v.Summary
						fixedIn = firstFixed(v.Affected)
						break
					}
				}
				findings = append(findings, Finding{
					CVEID:            cveID,
					PackageName:      pkg.Package.Name,
					InstalledVersion: pkg.Package.Version,
					FixedInVersion:   fixedIn,
					Severity:         normaliseSeverity(grp.MaxSeverity),
					Ecosystem:        pkg.Package.Ecosystem,
					Summary:          summary,
					ScanSource:       src,
				})
			}
		}
	}

	return ScanResult{Findings: findings, ScannedAt: time.Now().UTC(), LockFiles: lockfiles}, nil
}

func firstCVE(ids []string) string {
	for _, id := range ids {
		if len(id) >= 4 && id[:4] == "CVE-" {
			return id
		}
	}
	if len(ids) > 0 {
		return ids[0]
	}
	return ""
}

func idInSlice(id string, ids []string) bool {
	for _, g := range ids {
		if g == id {
			return true
		}
	}
	return false
}

func anyAliasInSlice(aliases, ids []string) bool {
	for _, a := range aliases {
		if idInSlice(a, ids) {
			return true
		}
	}
	return false
}

func firstFixed(affected []struct {
	Ranges []struct {
		Events []struct {
			Fixed string `json:"fixed,omitempty"`
		} `json:"events"`
	} `json:"ranges"`
}) string {
	for _, a := range affected {
		for _, r := range a.Ranges {
			for _, e := range r.Events {
				if e.Fixed != "" {
					return e.Fixed
				}
			}
		}
	}
	return ""
}

func normaliseSeverity(s string) string {
	switch s {
	case "CRITICAL", "critical":
		return "CRITICAL"
	case "HIGH", "high":
		return "HIGH"
	case "MEDIUM", "medium", "MODERATE", "moderate":
		return "MEDIUM"
	case "LOW", "low":
		return "LOW"
	default:
		if s != "" {
			return s
		}
		return "UNKNOWN"
	}
}

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

// Package rules contains local, deterministic startup-scan rule definitions.
package rules

// Severity is the startup scanner disposition for a finding.
type Severity string

const (
	SeverityInfo  Severity = "info"
	SeverityWarn  Severity = "warn"
	SeverityBlock Severity = "block"
)

// Category groups startup findings for later CLI and daemon rendering.
type Category string

const (
	CategoryClientProfile Category = "client-profile"
	CategoryIOC           Category = "ioc"
	CategoryPackage       Category = "package"
	CategoryPersistence   Category = "persistence"
	CategoryPolicy        Category = "policy"
)

// MatchType describes the scanner surface a rule applies to.
type MatchType string

const (
	MatchPath          MatchType = "path"
	MatchJSON          MatchType = "json"
	MatchPackageScript MatchType = "package-script"
	MatchCommand       MatchType = "command"
	MatchDomainIP      MatchType = "domain-ip"
	MatchServiceUnit   MatchType = "service-unit"
)

// Rule is a bundled startup scan rule. The fields are intentionally simple so
// the first implementation can stay local and later rule pack loading can map
// YAML/JSON into the same shape.
type Rule struct {
	ID          string
	Title       string
	Category    Category
	Severity    Severity
	MatchType   MatchType
	Patterns    []string
	Description string
	Remediation string
}

// Bundled returns the built-in startup scan rules.
func Bundled() []Rule {
	out := make([]Rule, len(bundled))
	copy(out, bundled)
	return out
}

var bundled = []Rule{
	{
		ID:          "ioc.mini-shai-hulud.file",
		Title:       "Suspicious AI-agent IOC filename",
		Category:    CategoryIOC,
		Severity:    SeverityBlock,
		MatchType:   MatchPath,
		Patterns:    []string{"router_init.js", "router_runtime.js", "setup.mjs", "tanstack_runner.js"},
		Description: "Known suspicious filenames associated with AI-agent package compromise campaigns.",
		Remediation: "Inspect the file before running agents or package scripts; quarantine it if unexpected.",
	},
	{
		ID:          "ioc.mini-shai-hulud.network",
		Title:       "Suspicious AI-agent IOC network indicator",
		Category:    CategoryIOC,
		Severity:    SeverityBlock,
		MatchType:   MatchDomainIP,
		Patterns:    []string{"git-tanstack.com", "getsession.org", "83.142.209.194"},
		Description: "Known suspicious domains or IPs associated with AI-agent package compromise campaigns.",
		Remediation: "Remove the reference and audit recent package or agent activity.",
	},
	{
		ID:          "persist.gh-token-monitor",
		Title:       "Suspicious token monitor persistence",
		Category:    CategoryPersistence,
		Severity:    SeverityBlock,
		MatchType:   MatchServiceUnit,
		Patterns:    []string{"gh-token-monitor.service", "com.user.gh-token-monitor.plist", "gh-token-monitor"},
		Description: "Suspicious persistence targeting GitHub tokens.",
		Remediation: "Disable the unit or LaunchAgent after preserving evidence for review.",
	},
	{
		ID:          "client.vscode.folder-open-task",
		Title:       "VS Code task runs automatically on folder open",
		Category:    CategoryClientProfile,
		Severity:    SeverityWarn,
		MatchType:   MatchJSON,
		Patterns:    []string{"runOptions.runOn=folderOpen"},
		Description: "VS Code can run this workspace task when the folder opens.",
		Remediation: "Remove runOptions.runOn or require explicit task execution.",
	},
	{
		ID:          "pkg.lifecycle-script",
		Title:       "Package lifecycle script executes during install",
		Category:    CategoryPackage,
		Severity:    SeverityWarn,
		MatchType:   MatchPackageScript,
		Patterns:    []string{"preinstall", "install", "postinstall", "prepare", "prepublish", "prepublishOnly"},
		Description: "Package manager lifecycle scripts can execute code before an agent reviews it.",
		Remediation: "Review the script and use package-manager settings that disable implicit scripts when possible.",
	},
	{
		ID:          "pkg.github-commit-dependency",
		Title:       "Package manifest references a GitHub commit dependency",
		Category:    CategoryPackage,
		Severity:    SeverityWarn,
		MatchType:   MatchCommand,
		Patterns:    []string{"github:"},
		Description: "GitHub commit dependencies bypass normal registry release and age controls.",
		Remediation: "Prefer pinned registry packages or vetted tarball checksums.",
	},
}

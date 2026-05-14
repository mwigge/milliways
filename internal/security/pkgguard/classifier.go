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

// Package pkgguard classifies package-manager commands that can introduce or
// mutate dependencies before agents have reviewed the resulting change.
package pkgguard

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Ecosystem identifies the dependency manager family for a command.
type Ecosystem string

const (
	EcosystemUnknown Ecosystem = "unknown"
	EcosystemNode    Ecosystem = "node"
	EcosystemPython  Ecosystem = "python"
	EcosystemGo      Ecosystem = "go"
	EcosystemRust    Ecosystem = "rust"
)

// Severity is the guard disposition for a classified command.
type Severity string

const (
	SeverityInfo  Severity = "info"
	SeverityWarn  Severity = "warn"
	SeverityBlock Severity = "block"
)

// ReasonCode is a stable machine-readable reason for a package guard hit.
type ReasonCode string

const (
	ReasonDependencyMutation ReasonCode = "dependency-mutation"
	ReasonInstallExec        ReasonCode = "install-exec"
	ReasonRemoteDependency   ReasonCode = "remote-dependency"
	ReasonEditableInstall    ReasonCode = "editable-install"
	ReasonUnpinnedInstall    ReasonCode = "unpinned-install"
)

// Finding describes one package-guard concern found in a command.
type Finding struct {
	Code     ReasonCode
	Severity Severity
	Message  string
	Subject  string
}

// Classification is the package-guard result for a command.
type Classification struct {
	Matched       bool
	Ecosystem     Ecosystem
	Manager       string
	Action        string
	DependencyOp  bool
	RequiresGuard bool
	Findings      []Finding
}

// ClassifyArgv classifies a tokenized command. It does not invoke package
// managers or inspect the filesystem.
func ClassifyArgv(argv []string) Classification {
	argv = compactArgv(argv)
	if len(argv) == 0 {
		return Classification{}
	}

	exe := commandName(argv[0])
	switch exe {
	case "npm", "pnpm", "yarn", "bun":
		return classifyNode(exe, argv[1:])
	case "pip", "pip3":
		return classifyPip(exe, argv[1:])
	case "python", "python3", "py":
		return classifyPython(argv)
	case "uv":
		return classifyUV(argv[1:])
	case "poetry":
		return classifyPoetry(argv[1:])
	case "go":
		return classifyGo(argv[1:])
	case "cargo":
		return classifyCargo(argv[1:])
	default:
		return Classification{}
	}
}

// ClassifyCommandLine tokenizes a simple shell command and classifies the first
// command segment. It intentionally avoids shell expansion and returns no match
// when tokenization cannot produce an argv-like form.
func ClassifyCommandLine(command string) Classification {
	argv := shellFields(command)
	return ClassifyArgv(argv)
}

func classifyNode(manager string, args []string) Classification {
	action := firstNonFlag(args)
	switch action {
	case "install", "i", "add", "update", "upgrade", "ci":
		c := Classification{
			Matched:       true,
			Ecosystem:     EcosystemNode,
			Manager:       manager,
			Action:        action,
			DependencyOp:  true,
			RequiresGuard: true,
		}
		msg := "package manager command can install or mutate dependencies"
		if action == "ci" {
			msg = "package manager command installs from the lockfile and may execute lifecycle scripts"
		}
		c.Findings = append(c.Findings, Finding{
			Code:     ReasonInstallExec,
			Severity: SeverityWarn,
			Message:  msg,
			Subject:  manager + " " + action,
		})
		return c
	default:
		return Classification{}
	}
}

func classifyPip(manager string, args []string) Classification {
	action := firstNonFlag(args)
	if action != "install" {
		return Classification{}
	}
	c := Classification{
		Matched:       true,
		Ecosystem:     EcosystemPython,
		Manager:       manager,
		Action:        action,
		DependencyOp:  true,
		RequiresGuard: true,
	}
	inspectPythonInstallSpecs(&c, args)
	return c
}

func classifyPython(argv []string) Classification {
	args := argv[1:]
	if len(args) < 3 || args[0] != "-m" || commandName(args[1]) != "pip" {
		return Classification{}
	}
	return classifyPip(argv[0]+" -m pip", args[2:])
}

func classifyUV(args []string) Classification {
	action := firstNonFlag(args)
	switch action {
	case "pip":
		rest := afterFirstNonFlag(args)
		if firstNonFlag(rest) != "install" {
			return Classification{}
		}
		c := Classification{
			Matched:       true,
			Ecosystem:     EcosystemPython,
			Manager:       "uv pip",
			Action:        "install",
			DependencyOp:  true,
			RequiresGuard: true,
		}
		inspectPythonInstallSpecs(&c, rest)
		return c
	case "add", "sync", "lock":
		return Classification{
			Matched:       true,
			Ecosystem:     EcosystemPython,
			Manager:       "uv",
			Action:        action,
			DependencyOp:  true,
			RequiresGuard: true,
			Findings: []Finding{{
				Code:     ReasonDependencyMutation,
				Severity: SeverityWarn,
				Message:  "uv command can change project dependencies or the lockfile",
				Subject:  "uv " + action,
			}},
		}
	default:
		return Classification{}
	}
}

func classifyPoetry(args []string) Classification {
	action := firstNonFlag(args)
	switch action {
	case "add", "update", "install", "lock":
		c := Classification{
			Matched:       true,
			Ecosystem:     EcosystemPython,
			Manager:       "poetry",
			Action:        action,
			DependencyOp:  true,
			RequiresGuard: true,
		}
		if action == "add" {
			inspectPythonInstallSpecs(&c, args)
		}
		if len(c.Findings) == 0 {
			c.Findings = append(c.Findings, Finding{
				Code:     ReasonDependencyMutation,
				Severity: SeverityWarn,
				Message:  "poetry command can install or mutate dependencies",
				Subject:  "poetry " + action,
			})
		}
		return c
	default:
		return Classification{}
	}
}

func classifyGo(args []string) Classification {
	action := firstNonFlag(args)
	switch action {
	case "get":
		return dependencyMutation(EcosystemGo, "go", action, "go get can add, upgrade, downgrade, or remove module requirements")
	case "install":
		return dependencyMutation(EcosystemGo, "go", action, "go install downloads and builds an external module when given a module version")
	case "mod":
		rest := afterFirstNonFlag(args)
		sub := firstNonFlag(rest)
		switch sub {
		case "tidy", "download", "vendor", "edit":
			c := dependencyMutation(EcosystemGo, "go", "mod "+sub, "go mod command can change or hydrate module dependencies")
			return c
		default:
			return Classification{}
		}
	default:
		return Classification{}
	}
}

func classifyCargo(args []string) Classification {
	action := firstNonFlag(args)
	switch action {
	case "add", "update", "install":
		return dependencyMutation(EcosystemRust, "cargo", action, "cargo command can add, update, or install dependencies")
	default:
		return Classification{}
	}
}

func dependencyMutation(ecosystem Ecosystem, manager, action, message string) Classification {
	return Classification{
		Matched:       true,
		Ecosystem:     ecosystem,
		Manager:       manager,
		Action:        action,
		DependencyOp:  true,
		RequiresGuard: true,
		Findings: []Finding{{
			Code:     ReasonDependencyMutation,
			Severity: SeverityWarn,
			Message:  message,
			Subject:  strings.TrimSpace(manager + " " + action),
		}},
	}
}

func inspectPythonInstallSpecs(c *Classification, args []string) {
	specs := pythonPackageSpecs(args)
	seen := map[ReasonCode]map[string]struct{}{}
	add := func(code ReasonCode, severity Severity, msg, subject string) {
		if seen[code] == nil {
			seen[code] = map[string]struct{}{}
		}
		if _, ok := seen[code][subject]; ok {
			return
		}
		seen[code][subject] = struct{}{}
		c.Findings = append(c.Findings, Finding{Code: code, Severity: severity, Message: msg, Subject: subject})
	}
	for _, spec := range specs {
		switch {
		case isEditableSpec(spec):
			add(ReasonEditableInstall, SeverityWarn, "editable Python installs execute local or remote project code during build", spec)
		case isRemoteSpec(spec):
			add(ReasonRemoteDependency, SeverityWarn, "Python install references a URL or Git dependency", spec)
		case isUnpinnedPythonSpec(spec):
			add(ReasonUnpinnedInstall, SeverityWarn, "Python install target is not pinned to an exact version", spec)
		}
	}
	if len(c.Findings) == 0 {
		c.Findings = append(c.Findings, Finding{
			Code:     ReasonDependencyMutation,
			Severity: SeverityInfo,
			Message:  "Python install command changes the active environment",
			Subject:  c.Manager + " " + c.Action,
		})
	}
}

func pythonPackageSpecs(args []string) []string {
	var specs []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			specs = append(specs, args[i+1:]...)
			break
		}
		if arg == "-e" || arg == "--editable" {
			if i+1 < len(args) {
				specs = append(specs, "-e "+args[i+1])
				i++
			}
			continue
		}
		if strings.HasPrefix(arg, "-e") && len(arg) > 2 {
			specs = append(specs, "-e "+strings.TrimSpace(arg[2:]))
			continue
		}
		if strings.HasPrefix(arg, "--editable=") {
			specs = append(specs, "-e "+strings.TrimPrefix(arg, "--editable="))
			continue
		}
		if strings.HasPrefix(arg, "-") {
			if flagTakesValue(arg) && i+1 < len(args) {
				i++
			}
			continue
		}
		if arg == "install" || arg == "add" {
			continue
		}
		specs = append(specs, arg)
	}
	sort.Strings(specs)
	return specs
}

func flagTakesValue(flag string) bool {
	switch flag {
	case "-r", "--requirement", "-c", "--constraint", "-i", "--index-url", "--extra-index-url", "-f", "--find-links", "--python":
		return true
	default:
		return false
	}
}

func isEditableSpec(spec string) bool {
	return strings.HasPrefix(spec, "-e ")
}

func isRemoteSpec(spec string) bool {
	s := strings.TrimPrefix(spec, "-e ")
	lower := strings.ToLower(s)
	return strings.HasPrefix(lower, "git+") ||
		strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "ssh://") ||
		strings.HasPrefix(lower, "git://") ||
		strings.Contains(lower, "@git+") ||
		strings.Contains(lower, "@ git+") ||
		strings.Contains(lower, "@ http://") ||
		strings.Contains(lower, "@ https://")
}

var exactPythonVersion = regexp.MustCompile(`(?i)(^|[^!<>=~])==[^=].+`)

func isUnpinnedPythonSpec(spec string) bool {
	s := strings.TrimSpace(strings.TrimPrefix(spec, "-e "))
	if s == "" || isRemoteSpec(s) || strings.HasPrefix(s, ".") || strings.HasPrefix(s, "/") {
		return false
	}
	if strings.Contains(s, "://") {
		return false
	}
	return !exactPythonVersion.MatchString(s)
}

func compactArgv(argv []string) []string {
	out := make([]string, 0, len(argv))
	for _, arg := range argv {
		if strings.TrimSpace(arg) != "" {
			out = append(out, arg)
		}
	}
	return out
}

func commandName(path string) string {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, ".exe")
	return strings.ToLower(base)
}

func firstNonFlag(args []string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			if i+1 < len(args) {
				return args[i+1]
			}
			return ""
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		return strings.ToLower(arg)
	}
	return ""
}

func afterFirstNonFlag(args []string) []string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			if i+1 < len(args) {
				return args[i+2:]
			}
			return nil
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		return args[i+1:]
	}
	return nil
}

func shellFields(s string) []string {
	var fields []string
	var b strings.Builder
	var quote rune
	escaped := false
	for _, r := range s {
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				b.WriteRune(r)
			}
			continue
		}
		switch r {
		case '\'', '"':
			quote = r
		case ' ', '\t', '\n', '\r':
			if b.Len() > 0 {
				fields = append(fields, b.String())
				b.Reset()
			}
		case ';', '|', '&':
			if b.Len() > 0 {
				fields = append(fields, b.String())
			}
			return fields
		default:
			b.WriteRune(r)
		}
	}
	if b.Len() > 0 {
		fields = append(fields, b.String())
	}
	return fields
}

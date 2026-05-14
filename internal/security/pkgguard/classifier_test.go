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

package pkgguard_test

import (
	"testing"

	"github.com/mwigge/milliways/internal/security/pkgguard"
)

func TestClassifyNodePackageManagers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		argv    []string
		manager string
		action  string
	}{
		{name: "npm install", argv: []string{"npm", "install"}, manager: "npm", action: "install"},
		{name: "npm alias", argv: []string{"npm", "i", "left-pad"}, manager: "npm", action: "i"},
		{name: "pnpm add", argv: []string{"pnpm", "add", "react"}, manager: "pnpm", action: "add"},
		{name: "yarn update", argv: []string{"yarn", "update"}, manager: "yarn", action: "update"},
		{name: "bun ci", argv: []string{"bun", "ci"}, manager: "bun", action: "ci"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := pkgguard.ClassifyArgv(tt.argv)
			if !got.Matched || !got.RequiresGuard || !got.DependencyOp {
				t.Fatalf("classification = %+v, want guarded dependency op", got)
			}
			if got.Ecosystem != pkgguard.EcosystemNode || got.Manager != tt.manager || got.Action != tt.action {
				t.Fatalf("classification = %+v, want node %s %s", got, tt.manager, tt.action)
			}
			if !hasFinding(got, pkgguard.ReasonInstallExec) {
				t.Fatalf("findings = %+v, want install-exec", got.Findings)
			}
		})
	}
}

func TestClassifyPythonInstallRisks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		argv []string
		code pkgguard.ReasonCode
	}{
		{name: "pip git", argv: []string{"pip", "install", "git+https://github.com/acme/pkg"}, code: pkgguard.ReasonRemoteDependency},
		{name: "pip url", argv: []string{"pip3", "install", "https://example.test/pkg.tar.gz"}, code: pkgguard.ReasonRemoteDependency},
		{name: "pip editable", argv: []string{"python", "-m", "pip", "install", "-e", "."}, code: pkgguard.ReasonEditableInstall},
		{name: "pip unpinned", argv: []string{"pip", "install", "requests>=2"}, code: pkgguard.ReasonUnpinnedInstall},
		{name: "uv pip unpinned", argv: []string{"uv", "pip", "install", "ruff"}, code: pkgguard.ReasonUnpinnedInstall},
		{name: "poetry add git", argv: []string{"poetry", "add", "pkg@git+https://github.com/acme/pkg"}, code: pkgguard.ReasonRemoteDependency},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := pkgguard.ClassifyArgv(tt.argv)
			if !got.Matched || got.Ecosystem != pkgguard.EcosystemPython || !got.RequiresGuard {
				t.Fatalf("classification = %+v, want guarded Python op", got)
			}
			if !hasFinding(got, tt.code) {
				t.Fatalf("findings = %+v, want %s", got.Findings, tt.code)
			}
		})
	}
}

func TestClassifyPythonPinnedInstallStillMatches(t *testing.T) {
	t.Parallel()

	got := pkgguard.ClassifyArgv([]string{"pip", "install", "requests==2.31.0"})
	if !got.Matched || !got.RequiresGuard {
		t.Fatalf("classification = %+v, want guarded install", got)
	}
	if !hasFinding(got, pkgguard.ReasonDependencyMutation) {
		t.Fatalf("findings = %+v, want dependency mutation info", got.Findings)
	}
	if hasFinding(got, pkgguard.ReasonUnpinnedInstall) {
		t.Fatalf("findings = %+v, did not want unpinned install", got.Findings)
	}
}

func TestClassifyGoAndCargoDependencyCommands(t *testing.T) {
	t.Parallel()

	tests := []struct {
		argv      []string
		ecosystem pkgguard.Ecosystem
		action    string
	}{
		{argv: []string{"go", "get", "example.com/mod@latest"}, ecosystem: pkgguard.EcosystemGo, action: "get"},
		{argv: []string{"go", "mod", "tidy"}, ecosystem: pkgguard.EcosystemGo, action: "mod tidy"},
		{argv: []string{"cargo", "add", "serde"}, ecosystem: pkgguard.EcosystemRust, action: "add"},
		{argv: []string{"cargo", "update"}, ecosystem: pkgguard.EcosystemRust, action: "update"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.action, func(t *testing.T) {
			t.Parallel()

			got := pkgguard.ClassifyArgv(tt.argv)
			if !got.Matched || !got.RequiresGuard || got.Ecosystem != tt.ecosystem || got.Action != tt.action {
				t.Fatalf("classification = %+v, want %s %s", got, tt.ecosystem, tt.action)
			}
			if !hasFinding(got, pkgguard.ReasonDependencyMutation) {
				t.Fatalf("findings = %+v, want dependency mutation", got.Findings)
			}
		})
	}
}

func TestClassifyCommandLineStopsAtFirstShellSegment(t *testing.T) {
	t.Parallel()

	got := pkgguard.ClassifyCommandLine("echo ok && npm install")
	if got.Matched {
		t.Fatalf("classification = %+v, want first shell segment only", got)
	}

	got = pkgguard.ClassifyCommandLine("pip install 'pkg>=1'")
	if !got.Matched || !hasFinding(got, pkgguard.ReasonUnpinnedInstall) {
		t.Fatalf("classification = %+v, want unpinned pip install", got)
	}
}

func TestClassifyIgnoresNonDependencyCommands(t *testing.T) {
	t.Parallel()

	for _, argv := range [][]string{
		{"npm", "test"},
		{"go", "test", "./..."},
		{"cargo", "test"},
		{"pip", "list"},
	} {
		if got := pkgguard.ClassifyArgv(argv); got.Matched {
			t.Fatalf("ClassifyArgv(%v) = %+v, want no match", argv, got)
		}
	}
}

func hasFinding(c pkgguard.Classification, code pkgguard.ReasonCode) bool {
	for _, f := range c.Findings {
		if f.Code == code {
			return true
		}
	}
	return false
}

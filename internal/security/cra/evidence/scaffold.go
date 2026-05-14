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

package evidence

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Options controls CRA evidence scaffold generation.
type Options struct {
	Workspace string
	DryRun    bool
	Force     bool
}

// Action describes what Scaffold did, or would do, for one evidence file.
type Action struct {
	RelPath string
	Path    string
	Status  string
}

// Result summarizes CRA evidence scaffold actions.
type Result struct {
	Workspace   string
	Actions     []Action
	Created     int
	Existing    int
	Overwritten int
}

type templateFile struct {
	relPath string
	content string
}

// Scaffold creates missing CRA evidence files in a workspace. Existing files
// are left untouched unless Force is set.
func Scaffold(opts Options) (Result, error) {
	workspace := strings.TrimSpace(opts.Workspace)
	if workspace == "" {
		workspace = "."
	}
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return Result{}, fmt.Errorf("resolve workspace: %w", err)
	}

	result := Result{
		Workspace: absWorkspace,
		Actions:   make([]Action, 0, len(scaffoldFiles)),
	}
	for _, file := range scaffoldFiles {
		path := filepath.Join(absWorkspace, filepath.FromSlash(file.relPath))
		exists, err := fileExists(path)
		if err != nil {
			return result, err
		}

		status := "create"
		switch {
		case exists && opts.Force:
			status = "overwrite"
			result.Overwritten++
		case exists:
			status = "exists"
			result.Existing++
		default:
			result.Created++
		}
		result.Actions = append(result.Actions, Action{RelPath: file.relPath, Path: path, Status: status})

		if opts.DryRun || status == "exists" {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return result, fmt.Errorf("create evidence directory %s: %w", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(file.content), 0o644); err != nil {
			return result, fmt.Errorf("write evidence file %s: %w", path, err)
		}
	}
	return result, nil
}

func fileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("stat evidence file %s: %w", path, err)
}

var scaffoldFiles = []templateFile{
	{
		relPath: "SECURITY.md",
		content: `# Security Policy

## Reporting a Vulnerability

Please do not open a public issue for security vulnerabilities.

Report security issues to: security@example.com

Include:
- A description of the vulnerability
- Reproduction steps or proof of concept
- Affected versions or deployments
- Potential impact
- Suggested fixes, if available

We aim to acknowledge reports within 7 days and provide status updates until the issue is resolved or accepted as risk.

## Scope

- Product source code and release artifacts
- Authentication, authorization, and secret handling
- Installer, update, and packaging flows
- Security scanner and vulnerability handling integrations

## Disclosure

Please allow a reasonable remediation window before public disclosure. We coordinate advisories, fixed versions, and credit with reporters when appropriate.
`,
	},
	{
		relPath: "SUPPORT.md",
		content: `# Support Policy

## Security Support

Security support is provided for the current stable release line.

Security support until: 2029-12-31

Supported surfaces include maintained source code, release artifacts, installer and update flows, documented configuration, and vulnerability handling processes.

Report vulnerabilities through the process in [SECURITY.md](SECURITY.md).
`,
	},
	{
		relPath: "docs/update-policy.md",
		content: `# Security Update Policy

## Update Channels

Security fixes are released through the normal project release channel unless an out-of-band advisory is required.

## Automatic Updates

Document whether the product updates automatically, prompts users to update, or requires manual installation. Include default behavior, opt-out behavior, and how users can verify the installed version.

## Vulnerability Handling

Security fixes should include an advisory or release note entry, affected version range, fixed version, severity, and mitigation guidance when available.

## Evidence

- Release notes or advisory links
- Fixed version tags
- SBOM path, when available
- Scanner results used before release
`,
	},
	{
		relPath: "docs/cra-technical-file.md",
		content: `# CRA Technical File

## Product

Name:
Version:
Maintainer:

## Security Risk Assessment

Document intended use, reasonably foreseeable misuse, assets, trust boundaries, and security risks considered during design and release.

## Secure-by-Default Evidence

- Default configuration
- Authentication and secret handling
- Network exposure
- Data storage and retention
- Logging and telemetry

## Vulnerability Handling Evidence

- Vulnerability reporting contact
- Triage and remediation process
- Coordinated disclosure policy
- Incident reporting workflow

## SBOM Evidence

Reference the machine-readable SBOM generated for release artifacts.

## Scanner Coverage

Record dependency, secret, and static-analysis scanners used before release, including versions and dates.

## Support and Update Posture

Reference [SUPPORT.md](../SUPPORT.md) and [update-policy.md](update-policy.md).
`,
	},
}

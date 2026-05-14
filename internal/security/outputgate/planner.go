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

// Package outputgate plans security scan requests for files produced by an
// agent or currently staged for commit. It does not invoke scanners.
package outputgate

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/mwigge/milliways/internal/security"
)

// ChangeSource identifies where a changed file list came from.
type ChangeSource string

const (
	SourceGenerated ChangeSource = "generated"
	SourceStaged    ChangeSource = "staged"
)

// ChangeStatus describes the changed file lifecycle state.
type ChangeStatus string

const (
	StatusAdded    ChangeStatus = "added"
	StatusModified ChangeStatus = "modified"
	StatusDeleted  ChangeStatus = "deleted"
	StatusRenamed  ChangeStatus = "renamed"
)

// FileChange is one generated or staged file to evaluate for scan planning.
type FileChange struct {
	Path   string       `json:"path"`
	Status ChangeStatus `json:"status"`
	Source ChangeSource `json:"source"`
}

// ScanRequest is a request for a later scanner invocation.
type ScanRequest struct {
	Kind   security.ScanKind `json:"kind"`
	Files  []string          `json:"files"`
	Reason string            `json:"reason"`
}

// Plan is the output gate's scanner worklist.
type Plan struct {
	Requests []ScanRequest `json:"requests"`
}

// PlanScans determines which scanner families should run for a set of changed
// files. Deleted files are ignored because scanners need current content.
func PlanScans(changes []FileChange) Plan {
	filesByKind := map[security.ScanKind]map[string]struct{}{}
	reasons := map[security.ScanKind]string{}
	add := func(kind security.ScanKind, path, reason string) {
		if filesByKind[kind] == nil {
			filesByKind[kind] = map[string]struct{}{}
			reasons[kind] = reason
		}
		filesByKind[kind][path] = struct{}{}
	}

	for _, change := range changes {
		path := normalizedPath(change.Path)
		if path == "" || change.Status == StatusDeleted {
			continue
		}
		if isDependencyFile(path) {
			add(security.ScanDependency, path, "dependency manifest or lockfile changed")
		}
		if isSecretRelevant(path) {
			add(security.ScanSecret, path, "changed file may contain credentials or tokens")
		}
		if isSASTRelevant(path) {
			add(security.ScanSAST, path, "changed source or executable configuration requires static analysis")
		}
	}

	order := []security.ScanKind{security.ScanSecret, security.ScanSAST, security.ScanDependency}
	var requests []ScanRequest
	for _, kind := range order {
		set := filesByKind[kind]
		if len(set) == 0 {
			continue
		}
		files := make([]string, 0, len(set))
		for path := range set {
			files = append(files, path)
		}
		sort.Strings(files)
		requests = append(requests, ScanRequest{Kind: kind, Files: files, Reason: reasons[kind]})
	}
	return Plan{Requests: requests}
}

func normalizedPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(path))
}

func isDependencyFile(path string) bool {
	base := filepath.Base(path)
	switch base {
	case "package.json", "package-lock.json", "npm-shrinkwrap.json", "pnpm-lock.yaml", "pnpm-workspace.yaml",
		"yarn.lock", "bun.lock", "bun.lockb", "requirements.txt", "requirements-dev.txt", "constraints.txt",
		"pyproject.toml", "poetry.lock", "pdm.lock", "uv.lock", "go.mod", "go.sum", "Cargo.toml", "Cargo.lock":
		return true
	default:
		return strings.HasSuffix(base, ".requirements.txt")
	}
}

func isSecretRelevant(path string) bool {
	if isDependencyFile(path) {
		return true
	}
	base := strings.ToLower(filepath.Base(path))
	if strings.HasPrefix(base, ".env") {
		return true
	}
	switch base {
	case "config.json", "settings.json", "credentials", "credentials.json", "secrets.json", "id_rsa", "id_ed25519":
		return true
	}
	switch strings.ToLower(filepath.Ext(base)) {
	case ".pem", ".key", ".p12", ".pfx", ".crt", ".cer", ".env", ".json", ".yaml", ".yml", ".toml", ".ini", ".conf", ".cfg", ".properties":
		return true
	default:
		return isSourceLike(path)
	}
}

func isSASTRelevant(path string) bool {
	if isDependencyFile(path) {
		return false
	}
	base := filepath.Base(path)
	if base == "Dockerfile" || strings.HasPrefix(base, "Dockerfile.") {
		return true
	}
	if strings.HasPrefix(filepath.ToSlash(path), ".github/workflows/") {
		return true
	}
	return isSourceLike(path)
}

func isSourceLike(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go", ".rs", ".py", ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".sh", ".bash", ".zsh", ".ps1",
		".rb", ".php", ".java", ".kt", ".kts", ".cs", ".c", ".cc", ".cpp", ".h", ".hpp", ".swift", ".scala",
		".sql", ".lua", ".pl", ".r":
		return true
	default:
		return false
	}
}

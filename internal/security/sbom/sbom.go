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

package sbom

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type SPDXDocument struct {
	SPDXVersion       string        `json:"spdxVersion"`
	DataLicense       string        `json:"dataLicense"`
	SPDXID            string        `json:"SPDXID"`
	Name              string        `json:"name"`
	DocumentNamespace string        `json:"documentNamespace"`
	CreationInfo      CreationInfo  `json:"creationInfo"`
	Packages          []SPDXPackage `json:"packages"`
}

type CreationInfo struct {
	Created  string   `json:"created"`
	Creators []string `json:"creators"`
}

type SPDXPackage struct {
	Name             string            `json:"name"`
	SPDXID           string            `json:"SPDXID"`
	VersionInfo      string            `json:"versionInfo,omitempty"`
	DownloadLocation string            `json:"downloadLocation"`
	FilesAnalyzed    bool              `json:"filesAnalyzed"`
	ExternalRefs     []SPDXExternalRef `json:"externalRefs,omitempty"`
	Source           string            `json:"source,omitempty"`
}

type SPDXExternalRef struct {
	ReferenceCategory string `json:"referenceCategory"`
	ReferenceType     string `json:"referenceType"`
	ReferenceLocator  string `json:"referenceLocator"`
}

type GenerateOptions struct {
	Workspace string
	Now       time.Time
}

func GenerateSPDX(opts GenerateOptions) (SPDXDocument, error) {
	workspace := opts.Workspace
	if workspace == "" {
		workspace = "."
	}
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return SPDXDocument{}, err
	}
	now := opts.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	packages := []SPDXPackage{rootPackage(abs)}
	packages = append(packages, readGoPackages(abs)...)
	packages = append(packages, readCargoPackages(abs)...)
	packages = append(packages, readNPMPackages(abs)...)
	packages = append(packages, readPNPMPackages(abs)...)
	packages = append(packages, readYarnPackages(abs)...)
	packages = append(packages, readBunPackages(abs)...)
	packages = append(packages, readRequirementsPackages(abs)...)
	packages = append(packages, readPyprojectPackages(abs)...)
	packages = append(packages, readPythonLockPackages(abs, "poetry.lock")...)
	packages = append(packages, readPythonLockPackages(abs, "uv.lock")...)
	sort.Slice(packages, func(i, j int) bool {
		if packages[i].Name == packages[j].Name {
			return packages[i].VersionInfo < packages[j].VersionInfo
		}
		return packages[i].Name < packages[j].Name
	})
	sum := sha256.Sum256([]byte(abs))
	return SPDXDocument{
		SPDXVersion:       "SPDX-2.3",
		DataLicense:       "CC0-1.0",
		SPDXID:            "SPDXRef-DOCUMENT",
		Name:              filepath.Base(abs),
		DocumentNamespace: "https://milliways.local/sbom/" + hex.EncodeToString(sum[:8]),
		CreationInfo: CreationInfo{
			Created:  now.Format(time.RFC3339),
			Creators: []string{"Tool: milliwaysctl security sbom"},
		},
		Packages: packages,
	}, nil
}

func WriteSPDXJSON(w io.Writer, doc SPDXDocument) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}

func WriteSPDXJSONFile(path string, doc SPDXDocument) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	err = WriteSPDXJSON(f, doc)
	closeErr := f.Close()
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if closeErr != nil {
		return fmt.Errorf("close %s: %w", path, closeErr)
	}
	return nil
}

func rootPackage(workspace string) SPDXPackage {
	return SPDXPackage{
		Name:             filepath.Base(workspace),
		SPDXID:           "SPDXRef-Package-root",
		DownloadLocation: "NOASSERTION",
		FilesAnalyzed:    false,
		Source:           ".",
	}
}

func readGoPackages(workspace string) []SPDXPackage {
	data, err := os.ReadFile(filepath.Join(workspace, "go.mod"))
	if err != nil {
		return nil
	}
	var out []SPDXPackage
	inBlock := false
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "//") || line == "" {
			continue
		}
		if strings.HasPrefix(line, "require (") {
			inBlock = true
			continue
		}
		if inBlock && line == ")" {
			inBlock = false
			continue
		}
		if strings.HasPrefix(line, "require ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "require "))
		} else if !inBlock {
			continue
		}
		if idx := strings.Index(line, "//"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		out = append(out, packageRef("go", fields[0], fields[1], "go.mod"))
	}
	return out
}

func readCargoPackages(workspace string) []SPDXPackage {
	f, err := os.Open(filepath.Join(workspace, "Cargo.lock"))
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	var out []SPDXPackage
	var name, version string
	flush := func() {
		if name != "" && version != "" {
			out = append(out, packageRef("cargo", name, version, "Cargo.lock"))
		}
		name, version = "", ""
	}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "[[package]]" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "name = ") {
			name = trimTOMLString(strings.TrimPrefix(line, "name = "))
		}
		if strings.HasPrefix(line, "version = ") {
			version = trimTOMLString(strings.TrimPrefix(line, "version = "))
		}
	}
	flush()
	return out
}

func readNPMPackages(workspace string) []SPDXPackage {
	data, err := os.ReadFile(filepath.Join(workspace, "package-lock.json"))
	if err != nil {
		return nil
	}
	var lock struct {
		Packages map[string]struct {
			Version string `json:"version"`
		} `json:"packages"`
		Dependencies map[string]struct {
			Version string `json:"version"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []SPDXPackage
	for path, pkg := range lock.Packages {
		if path == "" || pkg.Version == "" {
			continue
		}
		name := npmPackageNameFromPath(path)
		if name == "" || seen[name+"@"+pkg.Version] {
			continue
		}
		seen[name+"@"+pkg.Version] = true
		out = append(out, packageRef("npm", name, pkg.Version, "package-lock.json"))
	}
	for name, pkg := range lock.Dependencies {
		if name == "" || pkg.Version == "" || seen[name+"@"+pkg.Version] {
			continue
		}
		seen[name+"@"+pkg.Version] = true
		out = append(out, packageRef("npm", name, pkg.Version, "package-lock.json"))
	}
	return out
}

func npmPackageNameFromPath(path string) string {
	path = strings.TrimPrefix(strings.TrimSpace(path), "node_modules/")
	if path == "" || strings.Contains(path, "/node_modules/") {
		return ""
	}
	parts := strings.Split(path, "/")
	if len(parts) == 0 {
		return ""
	}
	if strings.HasPrefix(parts[0], "@") && len(parts) >= 2 {
		return parts[0] + "/" + parts[1]
	}
	return parts[0]
}

func readPNPMPackages(workspace string) []SPDXPackage {
	data, err := os.ReadFile(filepath.Join(workspace, "pnpm-lock.yaml"))
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []SPDXPackage
	inPackages := false
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == "packages:" {
			inPackages = true
			continue
		}
		if !inPackages || !strings.HasSuffix(line, ":") {
			continue
		}
		key := strings.TrimSuffix(strings.Trim(line, `'"`), ":")
		name, version := parsePNPMPackageKey(key)
		id := name + "@" + version
		if name != "" && version != "" && !seen[id] {
			seen[id] = true
			out = append(out, packageRef("npm", name, version, "pnpm-lock.yaml"))
		}
	}
	return out
}

func parsePNPMPackageKey(key string) (string, string) {
	key = strings.Trim(strings.TrimSpace(key), `'"`)
	key = strings.TrimPrefix(key, "/")
	if idx := strings.Index(key, "("); idx >= 0 {
		key = key[:idx]
	}
	at := strings.LastIndex(key, "@")
	if at <= 0 || at == len(key)-1 {
		return "", ""
	}
	name := key[:at]
	version := strings.TrimPrefix(key[at+1:], "npm:")
	if strings.Contains(version, "/") {
		return "", ""
	}
	return name, version
}

func readYarnPackages(workspace string) []SPDXPackage {
	data, err := os.ReadFile(filepath.Join(workspace, "yarn.lock"))
	if err != nil {
		return nil
	}
	var out []SPDXPackage
	var name string
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(raw, " ") && strings.HasSuffix(line, ":") {
			name = parseYarnPackageName(strings.TrimSuffix(line, ":"))
			continue
		}
		if name != "" && (strings.HasPrefix(line, "version ") || strings.HasPrefix(line, "version:")) {
			version := strings.TrimPrefix(line, "version")
			version = strings.TrimPrefix(strings.TrimSpace(version), ":")
			version = strings.Trim(strings.TrimSpace(version), `"`)
			if version != "" {
				out = append(out, packageRef("npm", name, version, "yarn.lock"))
			}
			name = ""
		}
	}
	return out
}

func parseYarnPackageName(selector string) string {
	selector = strings.Trim(selector, `'"`)
	if strings.Contains(selector, ",") {
		selector = strings.TrimSpace(strings.Split(selector, ",")[0])
	}
	if strings.HasPrefix(selector, "@") {
		idx := strings.LastIndex(selector, "@")
		if idx > 0 {
			return selector[:idx]
		}
		return ""
	}
	name, _, ok := strings.Cut(selector, "@")
	if !ok {
		return ""
	}
	return name
}

func readBunPackages(workspace string) []SPDXPackage {
	data, err := os.ReadFile(filepath.Join(workspace, "bun.lock"))
	if err != nil {
		return nil
	}
	var lock struct {
		Packages map[string][]json.RawMessage `json:"packages"`
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil
	}
	var out []SPDXPackage
	for key, fields := range lock.Packages {
		name, version := parseBunPackageKey(key)
		if len(fields) > 0 {
			var spec string
			if err := json.Unmarshal(fields[0], &spec); err == nil {
				if specName, specVersion := parseBunPackageKey(spec); specName != "" && specVersion != "" {
					name, version = specName, specVersion
				}
			}
		}
		if name != "" && version != "" {
			out = append(out, packageRef("npm", name, version, "bun.lock"))
		}
	}
	return out
}

func parseBunPackageKey(key string) (string, string) {
	key = strings.TrimSpace(key)
	if idx := strings.Index(key, "("); idx >= 0 {
		key = key[:idx]
	}
	at := strings.LastIndex(key, "@")
	if at <= 0 || at == len(key)-1 {
		return "", ""
	}
	name := key[:at]
	version := key[at+1:]
	if strings.Contains(version, "/") || strings.Contains(version, ":") {
		return "", ""
	}
	return name, version
}

func readRequirementsPackages(workspace string) []SPDXPackage {
	data, err := os.ReadFile(filepath.Join(workspace, "requirements.txt"))
	if err != nil {
		return nil
	}
	var out []SPDXPackage
	for _, raw := range strings.Split(string(data), "\n") {
		name, version := pythonExactRequirement(raw)
		if name != "" && version != "" {
			out = append(out, packageRef("pypi", name, version, "requirements.txt"))
		}
	}
	return out
}

func readPyprojectPackages(workspace string) []SPDXPackage {
	data, err := os.ReadFile(filepath.Join(workspace, "pyproject.toml"))
	if err != nil {
		return nil
	}
	var out []SPDXPackage
	inDependencies := false
	for _, raw := range strings.Split(string(data), "\n") {
		line := stripInlineComment(strings.TrimSpace(raw))
		if line == "" {
			continue
		}
		if inDependencies {
			out = append(out, pythonPackageRefsFromDependencyLine(line, "pyproject.toml")...)
			if strings.Contains(line, "]") {
				inDependencies = false
			}
			continue
		}
		if strings.HasPrefix(line, "dependencies = [") || strings.Contains(line, ".dependencies = [") {
			inDependencies = true
			out = append(out, pythonPackageRefsFromDependencyLine(line, "pyproject.toml")...)
			if strings.Contains(line, "]") {
				inDependencies = false
			}
		}
	}
	return out
}

func readPythonLockPackages(workspace, filename string) []SPDXPackage {
	f, err := os.Open(filepath.Join(workspace, filename))
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	var out []SPDXPackage
	var name, version string
	flush := func() {
		if name != "" && version != "" {
			out = append(out, packageRef("pypi", name, version, filename))
		}
		name, version = "", ""
	}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "[[package]]" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "name = ") {
			name = trimTOMLString(strings.TrimPrefix(line, "name = "))
		}
		if strings.HasPrefix(line, "version = ") {
			version = trimTOMLString(strings.TrimPrefix(line, "version = "))
		}
	}
	flush()
	return out
}

func pythonPackageRefsFromDependencyLine(line, source string) []SPDXPackage {
	var out []SPDXPackage
	for _, part := range strings.Split(line, ",") {
		name, version := pythonExactRequirement(part)
		if name != "" && version != "" {
			out = append(out, packageRef("pypi", name, version, source))
		}
	}
	return out
}

func pythonExactRequirement(line string) (string, string) {
	line = stripInlineComment(line)
	if idx := strings.Index(line, ";"); idx >= 0 {
		line = strings.TrimSpace(line[:idx])
	}
	line = strings.Trim(strings.TrimSpace(line), `[]"`)
	if strings.HasPrefix(line, "-") || strings.HasPrefix(line, ".") || strings.Contains(line, "://") {
		return "", ""
	}
	name, version, ok := strings.Cut(line, "==")
	if !ok {
		return "", ""
	}
	name = strings.TrimSpace(name)
	if idx := strings.Index(name, "["); idx >= 0 {
		name = name[:idx]
	}
	version = strings.Trim(strings.TrimSpace(version), `"'`)
	if name == "" || version == "" || strings.ContainsAny(name, " <>~=!") || strings.ContainsAny(version, " <>~=!") {
		return "", ""
	}
	return name, version
}

func stripInlineComment(line string) string {
	if idx := strings.Index(line, "#"); idx >= 0 {
		line = line[:idx]
	}
	return strings.TrimSpace(line)
}

func packageRef(ecosystem, name, version, source string) SPDXPackage {
	return SPDXPackage{
		Name:             name,
		SPDXID:           "SPDXRef-Package-" + sanitizeID(ecosystem+"-"+name+"-"+version),
		VersionInfo:      version,
		DownloadLocation: "NOASSERTION",
		FilesAnalyzed:    false,
		ExternalRefs: []SPDXExternalRef{{
			ReferenceCategory: "PACKAGE-MANAGER",
			ReferenceType:     "purl",
			ReferenceLocator:  fmt.Sprintf("pkg:%s/%s@%s", ecosystem, name, version),
		}},
		Source: source,
	}
}

func trimTOMLString(s string) string {
	return strings.Trim(strings.TrimSpace(s), `"`)
}

func sanitizeID(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

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

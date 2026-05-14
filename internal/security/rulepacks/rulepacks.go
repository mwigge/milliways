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

// Package rulepacks loads local security rule packs with checksum validation.
package rulepacks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mwigge/milliways/internal/security/rules"
	"gopkg.in/yaml.v3"
)

const maxRulePackFileBytes = 2 * 1024 * 1024

// Source identifies where a loaded pack came from.
type Source string

const (
	SourceBundled   Source = "bundled"
	SourceUser      Source = "user"
	SourceWorkspace Source = "workspace"
)

// Manifest is the on-disk rule-pack manifest.
type Manifest struct {
	Name                    string `json:"name" yaml:"name"`
	Version                 string `json:"version" yaml:"version"`
	Checksum                string `json:"checksum" yaml:"checksum"`
	Source                  string `json:"source" yaml:"source"`
	MinimumMilliWaysVersion string `json:"minimum_milliways_version" yaml:"minimum_milliways_version"`
	RulesFile               string `json:"rules_file" yaml:"rules_file"`
}

// Pack is a validated rule pack and its decoded rules.
type Pack struct {
	Manifest     Manifest
	Source       Source
	Root         string
	ManifestPath string
	RulesPath    string
	Rules        []rules.Rule
}

// Options configures local pack discovery. Network use is disabled by default;
// AllowNetwork only permits manifests to declare a remote source, it does not
// fetch remote content.
type Options struct {
	BundledDirs   []string
	UserDirs      []string
	WorkspaceDirs []string
	AllowNetwork  bool
}

// LoadAll loads every manifest found in bundled, user, and workspace roots.
func LoadAll(opts Options) ([]Pack, error) {
	var packs []Pack
	for _, root := range opts.BundledDirs {
		loaded, err := LoadDir(root, SourceBundled, opts.AllowNetwork)
		if err != nil {
			return nil, err
		}
		packs = append(packs, loaded...)
	}
	for _, root := range opts.UserDirs {
		loaded, err := LoadDir(root, SourceUser, opts.AllowNetwork)
		if err != nil {
			return nil, err
		}
		packs = append(packs, loaded...)
	}
	for _, root := range opts.WorkspaceDirs {
		loaded, err := LoadDir(root, SourceWorkspace, opts.AllowNetwork)
		if err != nil {
			return nil, err
		}
		packs = append(packs, loaded...)
	}
	sort.Slice(packs, func(i, j int) bool {
		if packs[i].Source != packs[j].Source {
			return packs[i].Source < packs[j].Source
		}
		if packs[i].Manifest.Name != packs[j].Manifest.Name {
			return packs[i].Manifest.Name < packs[j].Manifest.Name
		}
		return packs[i].Manifest.Version < packs[j].Manifest.Version
	})
	return packs, nil
}

// LoadDir loads manifest.yaml, manifest.yml, or manifest.json files directly
// under root or one directory below root. Missing roots are ignored.
func LoadDir(root string, source Source, allowNetwork bool) ([]Pack, error) {
	if strings.TrimSpace(root) == "" {
		return nil, nil
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("rule pack root %q: %w", root, err)
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read rule pack root %q: %w", abs, err)
	}
	var manifests []string
	for _, name := range manifestNames() {
		path := filepath.Join(abs, name)
		if _, err := os.Stat(path); err == nil {
			manifests = append(manifests, path)
		}
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(abs, entry.Name())
		for _, name := range manifestNames() {
			path := filepath.Join(dir, name)
			if _, err := os.Stat(path); err == nil {
				manifests = append(manifests, path)
			}
		}
	}
	sort.Strings(manifests)

	packs := make([]Pack, 0, len(manifests))
	for _, manifestPath := range manifests {
		pack, err := LoadManifest(manifestPath, source, allowNetwork)
		if err != nil {
			return nil, err
		}
		packs = append(packs, pack)
	}
	return packs, nil
}

// LoadManifest loads and verifies a single rule-pack manifest.
func LoadManifest(manifestPath string, source Source, allowNetwork bool) (Pack, error) {
	manifestBytes, err := readBounded(manifestPath)
	if err != nil {
		return Pack{}, fmt.Errorf("read rule pack manifest %q: %w", manifestPath, err)
	}
	var manifest Manifest
	if err := yaml.Unmarshal(manifestBytes, &manifest); err != nil {
		return Pack{}, fmt.Errorf("decode rule pack manifest %q: %w", manifestPath, err)
	}
	if err := validateManifest(manifest, allowNetwork); err != nil {
		return Pack{}, fmt.Errorf("rule pack manifest %q: %w", manifestPath, err)
	}

	root := filepath.Dir(manifestPath)
	rulesPath := filepath.Clean(filepath.Join(root, filepath.FromSlash(manifest.RulesFile)))
	if !withinDir(root, rulesPath) {
		return Pack{}, fmt.Errorf("rule pack manifest %q: rules_file escapes pack root", manifestPath)
	}
	ruleBytes, err := readBounded(rulesPath)
	if err != nil {
		return Pack{}, fmt.Errorf("read rule pack rules %q: %w", rulesPath, err)
	}
	actual := checksum(ruleBytes)
	if !checksumMatches(manifest.Checksum, actual) {
		return Pack{}, fmt.Errorf("rule pack %q checksum mismatch: got %s, want %s", manifest.Name, actual, manifest.Checksum)
	}
	decoded, err := decodeRules(ruleBytes)
	if err != nil {
		return Pack{}, fmt.Errorf("decode rule pack rules %q: %w", rulesPath, err)
	}

	return Pack{
		Manifest:     manifest,
		Source:       source,
		Root:         root,
		ManifestPath: manifestPath,
		RulesPath:    rulesPath,
		Rules:        decoded,
	}, nil
}

func validateManifest(manifest Manifest, allowNetwork bool) error {
	if strings.TrimSpace(manifest.Name) == "" {
		return errors.New("name is required")
	}
	if strings.TrimSpace(manifest.Version) == "" {
		return errors.New("version is required")
	}
	if strings.TrimSpace(manifest.Checksum) == "" {
		return errors.New("checksum is required")
	}
	if strings.TrimSpace(manifest.RulesFile) == "" {
		return errors.New("rules_file is required")
	}
	if !allowNetwork && isRemoteSource(manifest.Source) {
		return fmt.Errorf("remote source %q rejected in offline mode", manifest.Source)
	}
	return nil
}

func decodeRules(data []byte) ([]rules.Rule, error) {
	var wrapped struct {
		Rules []rawRule `json:"rules" yaml:"rules"`
	}
	if err := yaml.Unmarshal(data, &wrapped); err != nil {
		return nil, err
	}
	raw := wrapped.Rules
	if len(raw) == 0 {
		var list []rawRule
		if err := yaml.Unmarshal(data, &list); err != nil {
			return nil, err
		}
		raw = list
	}
	out := make([]rules.Rule, 0, len(raw))
	for _, r := range raw {
		out = append(out, rules.Rule{
			ID:          r.ID,
			Title:       r.Title,
			Category:    rules.Category(r.Category),
			Severity:    rules.Severity(r.Severity),
			MatchType:   rules.MatchType(r.MatchType),
			Patterns:    append([]string(nil), r.Patterns...),
			Description: r.Description,
			Remediation: r.Remediation,
		})
	}
	return out, nil
}

type rawRule struct {
	ID          string   `json:"id" yaml:"id"`
	Title       string   `json:"title" yaml:"title"`
	Category    string   `json:"category" yaml:"category"`
	Severity    string   `json:"severity" yaml:"severity"`
	MatchType   string   `json:"match_type" yaml:"match_type"`
	Patterns    []string `json:"patterns" yaml:"patterns"`
	Description string   `json:"description" yaml:"description"`
	Remediation string   `json:"remediation" yaml:"remediation"`
}

func readBounded(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxRulePackFileBytes {
		return nil, fmt.Errorf("file too large")
	}
	return os.ReadFile(path)
}

func checksum(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func checksumMatches(want, got string) bool {
	want = strings.TrimSpace(strings.ToLower(want))
	got = strings.TrimSpace(strings.ToLower(got))
	if !strings.HasPrefix(want, "sha256:") {
		want = "sha256:" + want
	}
	return want == got
}

func isRemoteSource(source string) bool {
	source = strings.ToLower(strings.TrimSpace(source))
	return strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://")
}

func withinDir(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func manifestNames() []string {
	return []string{"manifest.yaml", "manifest.yml", "manifest.json"}
}

// MarshalRules writes rules in the rule-pack file shape. It is used by tests
// and future pack tooling to produce checksummed content.
func MarshalRules(ruleSet []rules.Rule) ([]byte, error) {
	raw := struct {
		Rules []rawRule `json:"rules"`
	}{Rules: make([]rawRule, 0, len(ruleSet))}
	for _, r := range ruleSet {
		raw.Rules = append(raw.Rules, rawRule{
			ID:          r.ID,
			Title:       r.Title,
			Category:    string(r.Category),
			Severity:    string(r.Severity),
			MatchType:   string(r.MatchType),
			Patterns:    append([]string(nil), r.Patterns...),
			Description: r.Description,
			Remediation: r.Remediation,
		})
	}
	return json.MarshalIndent(raw, "", "  ")
}

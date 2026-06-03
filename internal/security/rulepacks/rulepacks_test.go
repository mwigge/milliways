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

package rulepacks_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mwigge/milliways/internal/security/rulepacks"
	"github.com/mwigge/milliways/internal/security/rules"
)

func TestLoadAllLoadsBundledUserWorkspacePacks(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	bundled := filepath.Join(root, "bundled")
	user := filepath.Join(root, "user")
	workspace := filepath.Join(root, "workspace")
	writePack(t, filepath.Join(bundled, "ioc"), "bundled-ioc", "1.0.0", "")
	writePack(t, filepath.Join(user, "local"), "user-local", "1.1.0", "file://user")
	writePack(t, filepath.Join(workspace, "repo"), "workspace-repo", "2.0.0", "workspace")

	packs, err := rulepacks.LoadAll(rulepacks.Options{
		BundledDirs:   []string{bundled},
		UserDirs:      []string{user},
		WorkspaceDirs: []string{workspace},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(packs) != 3 {
		t.Fatalf("packs = %d, want 3: %#v", len(packs), packs)
	}

	seen := map[rulepacks.Source]bool{}
	for _, pack := range packs {
		seen[pack.Source] = true
		if len(pack.Rules) != 1 {
			t.Fatalf("pack %q rules = %d, want 1", pack.Manifest.Name, len(pack.Rules))
		}
		if pack.Rules[0].MatchType != rules.MatchPath {
			t.Fatalf("match type = %q", pack.Rules[0].MatchType)
		}
	}
	for _, source := range []rulepacks.Source{rulepacks.SourceBundled, rulepacks.SourceUser, rulepacks.SourceWorkspace} {
		if !seen[source] {
			t.Fatalf("missing source %q in %#v", source, packs)
		}
	}
}

func TestLoadManifestRejectsTamperedRules(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manifest := writePack(t, root, "tampered", "1.0.0", "")
	if err := os.WriteFile(filepath.Join(root, "rules.yaml"), []byte("rules: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := rulepacks.LoadManifest(manifest, rulepacks.SourceUser, false)
	if err == nil {
		t.Fatal("expected checksum mismatch")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadManifestRejectsRemoteSourceOffline(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manifest := writePack(t, root, "remote", "1.0.0", "https://updates.example.test/rules")

	_, err := rulepacks.LoadManifest(manifest, rulepacks.SourceBundled, false)
	if err == nil {
		t.Fatal("expected offline remote-source rejection")
	}
	if !strings.Contains(err.Error(), "offline mode") {
		t.Fatalf("error = %v", err)
	}

	if _, err := rulepacks.LoadManifest(manifest, rulepacks.SourceBundled, true); err != nil {
		t.Fatalf("allow network should permit remote metadata: %v", err)
	}
}

func TestLoadManifestRejectsEscapingRulesFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	data := []byte("rules: []\n")
	if err := os.WriteFile(filepath.Join(root, "outside.yaml"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "pack", "manifest.yaml")
	if err := os.MkdirAll(filepath.Dir(manifest), 0o755); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`name: escape
version: 1.0.0
checksum: %s
source: workspace
minimum_milliways_version: 0.0.0
rules_file: ../outside.yaml
`, testChecksum(data))
	if err := os.WriteFile(manifest, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := rulepacks.LoadManifest(manifest, rulepacks.SourceWorkspace, false)
	if err == nil {
		t.Fatal("expected escaping rules_file rejection")
	}
	if !strings.Contains(err.Error(), "escapes pack root") {
		t.Fatalf("error = %v", err)
	}
}

func writePack(t *testing.T, root, name, version, source string) string {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	ruleBytes, err := rulepacks.MarshalRules([]rules.Rule{{
		ID:          "ioc.test",
		Title:       "Test IOC",
		Category:    rules.CategoryIOC,
		Severity:    rules.SeverityBlock,
		MatchType:   rules.MatchPath,
		Patterns:    []string{"setup.mjs"},
		Description: "test rule",
		Remediation: "remove test file",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "rules.yaml"), ruleBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "manifest.yaml")
	body := fmt.Sprintf(`name: %s
version: %s
checksum: %s
source: %s
minimum_milliways_version: 0.0.0
rules_file: rules.yaml
`, name, version, testChecksum(ruleBytes), source)
	if err := os.WriteFile(manifest, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func testChecksum(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

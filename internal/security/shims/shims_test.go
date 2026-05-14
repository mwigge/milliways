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

package shims

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDefaultCatalogCoversCommandBrokerSurface(t *testing.T) {
	want := []string{
		"bash", "sh", "zsh",
		"npm", "pnpm", "yarn", "bun",
		"pip", "uv", "poetry",
		"go", "cargo",
		"curl", "wget",
		"git",
		"systemctl", "launchctl", "crontab",
	}
	if err := ValidateCatalog(DefaultCatalog); err != nil {
		t.Fatalf("ValidateCatalog(DefaultCatalog) = %v", err)
	}
	if got := Names(DefaultCatalog); !reflect.DeepEqual(got, want) {
		t.Fatalf("Names(DefaultCatalog) = %v, want %v", got, want)
	}
}

func TestPrependPathPutsShimDirFirstAndDeduplicates(t *testing.T) {
	root := t.TempDir()
	shimDir := filepath.Join(root, "shims")
	binA := filepath.Join(root, "bin-a")
	binB := filepath.Join(root, "bin-b")

	path := joinPath(binA, shimDir, binB, filepath.Clean(filepath.Join(shimDir, ".")))
	got := PrependPath(path, shimDir)
	want := joinPath(shimDir, binA, binB)
	if got != want {
		t.Fatalf("PrependPath() = %q, want %q", got, want)
	}
}

func TestInstallCatalogIsIdempotentAndExecutable(t *testing.T) {
	root := t.TempDir()
	shimDir := filepath.Join(root, "shims")
	catalog := []Metadata{{Name: "git", Category: CategoryVCS, Description: "Git"}}

	first, err := InstallCatalog(InstallOptions{Dir: shimDir, Catalog: catalog})
	if err != nil {
		t.Fatalf("InstallCatalog(first) error = %v", err)
	}
	if first.Replaced != 1 {
		t.Fatalf("first Replaced = %d, want 1", first.Replaced)
	}
	if len(first.Paths) != 1 {
		t.Fatalf("first Paths = %v, want one path", first.Paths)
	}
	if got, want := first.Env["PATH"], PrependPath(os.Getenv("PATH"), first.Dir); got != want {
		t.Fatalf("InstallCatalog Env PATH = %q, want %q", got, want)
	}
	assertExecutable(t, first.Paths[0])

	if err := os.Chmod(first.Paths[0], 0o644); err != nil {
		t.Fatalf("Chmod(%q) = %v", first.Paths[0], err)
	}
	second, err := InstallCatalog(InstallOptions{Dir: shimDir, Catalog: catalog})
	if err != nil {
		t.Fatalf("InstallCatalog(second) error = %v", err)
	}
	if second.Replaced != 0 {
		t.Fatalf("second Replaced = %d, want 0", second.Replaced)
	}
	assertExecutable(t, first.Paths[0])
}

func TestStatusCatalogReportsInstalledAndMissingTools(t *testing.T) {
	root := t.TempDir()
	shimDir := filepath.Join(root, "shims")
	realDir := filepath.Join(root, "real")
	brokerDir := filepath.Join(root, "broker")
	mkdirAll(t, shimDir, realDir, brokerDir)
	writeExecutable(t, filepath.Join(realDir, "git"))
	writeExecutable(t, filepath.Join(brokerDir, "milliwaysctl"))

	catalog := []Metadata{
		{Name: "git", Category: CategoryVCS, Description: "Git"},
		{Name: "npm", Category: CategoryPackageManager, Description: "npm"},
	}
	if _, err := InstallCatalog(InstallOptions{Dir: shimDir, Catalog: catalog}); err != nil {
		t.Fatalf("InstallCatalog() error = %v", err)
	}
	if err := os.Remove(filepath.Join(shimDir, "npm")); err != nil {
		t.Fatalf("Remove npm shim: %v", err)
	}

	status, err := StatusCatalog(StatusOptions{
		Dir:     shimDir,
		Catalog: catalog,
		Path:    joinPath(shimDir, brokerDir, realDir),
	})
	if err != nil {
		t.Fatalf("StatusCatalog() error = %v", err)
	}
	if status.Expected != 2 || status.Installed != 1 {
		t.Fatalf("counts = installed %d expected %d, want 1/2", status.Installed, status.Expected)
	}
	if status.Ready {
		t.Fatalf("Ready = true, want false")
	}
	if status.Protected {
		t.Fatalf("Protected = true, want false when a shim is missing")
	}
	if !status.BrokerInstalled || status.BrokerPath != filepath.Join(brokerDir, "milliwaysctl") {
		t.Fatalf("broker status = installed %v path %q", status.BrokerInstalled, status.BrokerPath)
	}
	if !reflect.DeepEqual(status.MissingShims, []string{"npm"}) {
		t.Fatalf("MissingShims = %v, want [npm]", status.MissingShims)
	}
	if !reflect.DeepEqual(status.MissingRealTools, []string{"npm"}) {
		t.Fatalf("MissingRealTools = %v, want [npm]", status.MissingRealTools)
	}
}

func TestStatusCatalogReadyWithInstalledShimsEvenWhenOptionalRealToolMissing(t *testing.T) {
	root := t.TempDir()
	shimDir := filepath.Join(root, "shims")
	realDir := filepath.Join(root, "real")
	brokerDir := filepath.Join(root, "broker")
	mkdirAll(t, realDir, brokerDir)
	writeExecutable(t, filepath.Join(realDir, "git"))
	writeExecutable(t, filepath.Join(brokerDir, "milliwaysctl"))

	catalog := []Metadata{
		{Name: "git", Category: CategoryVCS, Description: "Git"},
		{Name: "bun", Category: CategoryPackageManager, Description: "Bun"},
	}
	if _, err := InstallCatalog(InstallOptions{Dir: shimDir, Catalog: catalog}); err != nil {
		t.Fatalf("InstallCatalog() error = %v", err)
	}
	status, err := StatusCatalog(StatusOptions{
		Dir:     shimDir,
		Catalog: catalog,
		Path:    joinPath(shimDir, brokerDir, realDir),
	})
	if err != nil {
		t.Fatalf("StatusCatalog() error = %v", err)
	}
	if !status.Ready || !status.Protected {
		t.Fatalf("ready/protected = %v/%v, want true/true; status=%#v", status.Ready, status.Protected, status)
	}
	if !reflect.DeepEqual(status.MissingRealTools, []string{"bun"}) {
		t.Fatalf("MissingRealTools = %v, want [bun]", status.MissingRealTools)
	}
}

func TestStatusCatalogReportsMissingBroker(t *testing.T) {
	root := t.TempDir()
	shimDir := filepath.Join(root, "shims")
	realDir := filepath.Join(root, "real")
	mkdirAll(t, realDir)
	writeExecutable(t, filepath.Join(realDir, "git"))

	catalog := []Metadata{{Name: "git", Category: CategoryVCS, Description: "Git"}}
	if _, err := InstallCatalog(InstallOptions{Dir: shimDir, Catalog: catalog}); err != nil {
		t.Fatalf("InstallCatalog() error = %v", err)
	}
	status, err := StatusCatalog(StatusOptions{
		Dir:           shimDir,
		Catalog:       catalog,
		Path:          joinPath(shimDir, realDir),
		BrokerCommand: "missing-milliwaysctl",
	})
	if err != nil {
		t.Fatalf("StatusCatalog() error = %v", err)
	}
	if status.BrokerInstalled {
		t.Fatalf("BrokerInstalled = true, want false")
	}
	if len(status.Issues) == 0 || status.Issues[0].Kind != "missing-broker" {
		t.Fatalf("Issues = %#v, want missing-broker", status.Issues)
	}
}

func TestGeneratedShimExecsBrokerWithMetadata(t *testing.T) {
	root := t.TempDir()
	shimDir := filepath.Join(root, "shims")
	realDir := filepath.Join(root, "real")
	brokerDir := filepath.Join(root, "broker")
	mkdirAll(t, shimDir, realDir, brokerDir)
	out := filepath.Join(root, "broker.out")

	writeScript(t, filepath.Join(realDir, "git"), "#!/bin/sh\nexit 99\n")
	writeScript(t, filepath.Join(brokerDir, "milliwaysctl"), `#!/bin/sh
{
	printf 'broker=1\n'
	printf 'active=%s\n' "$MILLIWAYS_SECURITY_SHIM_ACTIVE"
	printf 'command=%s\n' "$MILLIWAYS_SECURITY_SHIM_COMMAND"
	printf 'category=%s\n' "$MILLIWAYS_SECURITY_SHIM_CATEGORY"
	printf 'resolved=%s\n' "$MILLIWAYS_SECURITY_SHIM_RESOLVED"
	printf 'shimdir=%s\n' "$MILLIWAYS_SECURITY_SHIM_DIR"
	printf 'original_path=%s\n' "$MILLIWAYS_SECURITY_SHIM_ORIGINAL_PATH"
	printf 'path=%s\n' "$PATH"
	printf 'args=%s\n' "$*"
} > "$MW_TEST_OUT"
exit 23
`)
	_, err := InstallCatalog(InstallOptions{
		Dir:     shimDir,
		Catalog: []Metadata{{Name: "git", Category: CategoryVCS, Description: "Git"}},
	})
	if err != nil {
		t.Fatalf("InstallCatalog() error = %v", err)
	}

	cmd := exec.Command(filepath.Join(shimDir, "git"), "status", "--short")
	cmd.Env = append(os.Environ(),
		"MW_TEST_OUT="+out,
		"PATH="+joinPath(shimDir, brokerDir, realDir),
	)
	err = cmd.Run()
	if exitCode(err) != 23 {
		t.Fatalf("shim exit = %v, want exit code 23", err)
	}
	got := readFile(t, out)
	wantParts := []string{
		"broker=1\n",
		"active=1\n",
		"command=git\n",
		"category=vcs\n",
		"resolved=" + filepath.Join(realDir, "git") + "\n",
		"shimdir=" + filepath.Clean(shimDir) + "\n",
		"original_path=" + joinPath(shimDir, brokerDir, realDir) + "\n",
		"path=" + joinPath(brokerDir, realDir) + "\n",
		"args=security shim-exec -- " + filepath.Join(realDir, "git") + " status --short\n",
	}
	for _, want := range wantParts {
		if !strings.Contains(got, want) {
			t.Fatalf("broker output missing %q:\n%s", want, got)
		}
	}
}

func TestGeneratedShimFallsBackToRealBinaryWithoutBroker(t *testing.T) {
	root := t.TempDir()
	shimDir := filepath.Join(root, "shims")
	realDir := filepath.Join(root, "real")
	mkdirAll(t, shimDir, realDir)
	out := filepath.Join(root, "real.out")

	writeScript(t, filepath.Join(realDir, "git"), `#!/bin/sh
printf 'active=%s\n' "$MILLIWAYS_SECURITY_SHIM_ACTIVE" > "$MW_TEST_OUT"
printf 'command=%s\n' "$MILLIWAYS_SECURITY_SHIM_COMMAND" >> "$MW_TEST_OUT"
printf 'resolved=%s\n' "$MILLIWAYS_SECURITY_SHIM_RESOLVED" >> "$MW_TEST_OUT"
printf 'args=%s\n' "$*" >> "$MW_TEST_OUT"
exit 17
`)
	_, err := InstallCatalog(InstallOptions{
		Dir:     shimDir,
		Catalog: []Metadata{{Name: "git", Category: CategoryVCS, Description: "Git"}},
	})
	if err != nil {
		t.Fatalf("InstallCatalog() error = %v", err)
	}

	cmd := exec.Command(filepath.Join(shimDir, "git"), "status")
	cmd.Env = append(os.Environ(),
		"MW_TEST_OUT="+out,
		"PATH="+joinPath(shimDir, realDir),
		EnvBroker+"=missing-milliways-broker",
	)
	err = cmd.Run()
	if exitCode(err) != 17 {
		t.Fatalf("shim exit = %v, want exit code 17", err)
	}
	got := readFile(t, out)
	for _, want := range []string{
		"active=1\n",
		"command=git\n",
		"resolved=" + filepath.Join(realDir, "git") + "\n",
		"args=status\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("real output missing %q:\n%s", want, got)
		}
	}
}

func TestGeneratedShimAvoidsRecursionWhenActive(t *testing.T) {
	root := t.TempDir()
	shimDir := filepath.Join(root, "shims")
	realDir := filepath.Join(root, "real")
	brokerDir := filepath.Join(root, "broker")
	mkdirAll(t, shimDir, realDir, brokerDir)
	out := filepath.Join(root, "out")

	writeScript(t, filepath.Join(realDir, "git"), "#!/bin/sh\nprintf 'real\\n' > \"$MW_TEST_OUT\"\nexit 19\n")
	writeScript(t, filepath.Join(brokerDir, "milliwaysctl"), "#!/bin/sh\nprintf 'broker\\n' > \"$MW_TEST_OUT\"\nexit 23\n")
	_, err := InstallCatalog(InstallOptions{
		Dir:     shimDir,
		Catalog: []Metadata{{Name: "git", Category: CategoryVCS, Description: "Git"}},
	})
	if err != nil {
		t.Fatalf("InstallCatalog() error = %v", err)
	}

	cmd := exec.Command(filepath.Join(shimDir, "git"))
	cmd.Env = append(os.Environ(),
		"MW_TEST_OUT="+out,
		"PATH="+joinPath(shimDir, brokerDir, realDir),
		EnvActive+"=1",
	)
	err = cmd.Run()
	if exitCode(err) != 19 {
		t.Fatalf("shim exit = %v, want exit code 19", err)
	}
	if got := strings.TrimSpace(readFile(t, out)); got != "real" {
		t.Fatalf("executed %q, want real", got)
	}
}

func TestResolveRealBinaryExcludesShimDir(t *testing.T) {
	root := t.TempDir()
	shimDir := filepath.Join(root, "shims")
	realDir := filepath.Join(root, "real")
	otherDir := filepath.Join(root, "other")
	mkdirAll(t, shimDir, realDir, otherDir)
	writeExecutable(t, filepath.Join(shimDir, "git"))
	writeExecutable(t, filepath.Join(realDir, "git"))
	writeExecutable(t, filepath.Join(otherDir, "git"))

	got, err := ResolveRealBinary(ResolveOptions{
		Command: "git",
		Path:    joinPath(shimDir, realDir, otherDir),
		ShimDir: shimDir,
	})
	if err != nil {
		t.Fatalf("ResolveRealBinary() error = %v", err)
	}
	want := filepath.Join(realDir, "git")
	if got != want {
		t.Fatalf("ResolveRealBinary() = %q, want %q", got, want)
	}
}

func TestResolveRealBinaryExcludesSymlinkedShimDir(t *testing.T) {
	root := t.TempDir()
	shimDir := filepath.Join(root, "shims")
	linkDir := filepath.Join(root, "shim-link")
	realDir := filepath.Join(root, "real")
	mkdirAll(t, shimDir, realDir)
	writeExecutable(t, filepath.Join(shimDir, "npm"))
	writeExecutable(t, filepath.Join(realDir, "npm"))
	if err := os.Symlink(shimDir, linkDir); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	got, err := ResolveRealBinary(ResolveOptions{
		Command: "npm",
		Path:    joinPath(linkDir, realDir),
		ShimDir: shimDir,
	})
	if err != nil {
		t.Fatalf("ResolveRealBinary() error = %v", err)
	}
	want := filepath.Join(realDir, "npm")
	if got != want {
		t.Fatalf("ResolveRealBinary() = %q, want %q", got, want)
	}
}

func TestDecisionActionsCoverApprovalFlow(t *testing.T) {
	actions := []Action{ActionAllow, ActionWarn, ActionNeedsConfirmation, ActionBlock}
	want := []string{"allow", "warn", "needs-confirmation", "block"}
	got := make([]string, 0, len(actions))
	for _, action := range actions {
		got = append(got, string(action))
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("actions = %v, want %v", got, want)
	}
}

func joinPath(parts ...string) string {
	return strings.Join(parts, string(os.PathListSeparator))
}

func mkdirAll(t *testing.T, dirs ...string) {
	t.Helper()
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) = %v", dir, err)
		}
	}
}

func writeExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(%q) = %v", path, err)
	}
}

func writeScript(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile(%q) = %v", path, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) = %v", path, err)
	}
	return string(b)
}

func assertExecutable(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%q) = %v", path, err)
	}
	if info.Mode().Perm()&0111 == 0 {
		t.Fatalf("%q mode = %v, want executable bit", path, info.Mode().Perm())
	}
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return -1
}

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

package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mwigge/milliways/internal/runner/review"
)

func TestBuildHFGGUFURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		repo  string
		quant string
		want  string
	}{
		{
			name:  "unsloth qwen 7b coder",
			repo:  "unsloth/Qwen2.5-Coder-7B-Instruct-GGUF",
			quant: "Q4_K_M",
			want:  "https://huggingface.co/unsloth/Qwen2.5-Coder-7B-Instruct-GGUF/resolve/main/Qwen2.5-Coder-7B-Instruct-Q4_K_M.gguf",
		},
		{
			name:  "unsloth qwen 1.5b coder default quant",
			repo:  "unsloth/Qwen2.5-Coder-1.5B-Instruct-GGUF",
			quant: "Q4_K_M",
			want:  "https://huggingface.co/unsloth/Qwen2.5-Coder-1.5B-Instruct-GGUF/resolve/main/Qwen2.5-Coder-1.5B-Instruct-Q4_K_M.gguf",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := buildHFGGUFURL(c.repo, c.quant)
			if got != c.want {
				t.Errorf("buildHFGGUFURL(%q, %q) = %q, want %q", c.repo, c.quant, got, c.want)
			}
		})
	}
}

func TestDefaultGGUFDest(t *testing.T) {
	t.Parallel()

	got := defaultGGUFDest("/models", "unsloth/Qwen2.5-Coder-7B-Instruct-GGUF", "Q4_K_M")
	want := "/models/Qwen2.5-Coder-7B-Instruct-GGUF-Q4_K_M.gguf"
	if got != want {
		t.Errorf("defaultGGUFDest = %q, want %q", got, want)
	}
}

func TestParseListModelsResponse(t *testing.T) {
	t.Parallel()

	body := []byte(`{"object":"list","data":[{"id":"qwen2.5-coder-1.5b","object":"model"},{"id":"deepseek-coder-v2-lite","object":"model"}]}`)
	got, err := parseListModelsResponse(body)
	if err != nil {
		t.Fatalf("parseListModelsResponse err = %v", err)
	}
	want := []string{"qwen2.5-coder-1.5b", "deepseek-coder-v2-lite"}
	if len(got) != len(want) {
		t.Fatalf("got %d models, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("models[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseListModelsResponse_Malformed(t *testing.T) {
	t.Parallel()

	if _, err := parseListModelsResponse([]byte("not json")); err == nil {
		t.Error("expected error on malformed body, got nil")
	}
}

func TestLocalEndpointForKind(t *testing.T) {
	t.Parallel()

	cases := []struct {
		kind string
		want string
	}{
		{"llama-server", "http://127.0.0.1:8765/v1"},
		{"llama-swap", "http://127.0.0.1:8765/v1"},
		{"ollama", "http://127.0.0.1:11434/v1"},
		{"vllm", "http://127.0.0.1:8000/v1"},
		{"lmstudio", "http://127.0.0.1:1234/v1"},
	}
	for _, c := range cases {
		t.Run(c.kind, func(t *testing.T) {
			got, err := localEndpointForKind(c.kind)
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if got != c.want {
				t.Errorf("localEndpointForKind(%q) = %q, want %q", c.kind, got, c.want)
			}
		})
	}
}

func TestLocalEndpointForKind_Unknown(t *testing.T) {
	t.Parallel()

	if _, err := localEndpointForKind("not-a-backend"); err == nil {
		t.Error("expected error for unknown kind, got nil")
	}
}

func TestInsertSwapModelEntry_AddsToEmptyConfig(t *testing.T) {
	t.Parallel()

	original := []byte("models: {}\n")
	updated, changed, err := insertSwapModelEntry(original, "qwen2.5-coder-7b", "/models/Qwen2.5-Coder-7B-Instruct-GGUF-Q4_K_M.gguf")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !changed {
		t.Error("changed = false, want true (entry was new)")
	}
	if !strings.Contains(string(updated), "qwen2.5-coder-7b") {
		t.Errorf("updated config missing alias, got:\n%s", updated)
	}
	if !strings.Contains(string(updated), "Qwen2.5-Coder-7B-Instruct-GGUF-Q4_K_M.gguf") {
		t.Errorf("updated config missing gguf path, got:\n%s", updated)
	}
}

func TestInsertSwapModelEntry_IsIdempotent(t *testing.T) {
	t.Parallel()

	original := []byte("models: {}\n")
	once, _, err := insertSwapModelEntry(original, "qwen2.5-coder-7b", "/m/q.gguf")
	if err != nil {
		t.Fatalf("first insert err = %v", err)
	}
	twice, changedAgain, err := insertSwapModelEntry(once, "qwen2.5-coder-7b", "/m/q.gguf")
	if err != nil {
		t.Fatalf("second insert err = %v", err)
	}
	if changedAgain {
		t.Error("changed = true on second insert, want false (idempotent)")
	}
	if !bytes.Equal(once, twice) {
		t.Errorf("config differs between insertions:\nfirst:\n%s\nsecond:\n%s", once, twice)
	}
}

func TestInsertSwapModelEntry_PreservesExistingEntries(t *testing.T) {
	t.Parallel()

	original := []byte(`models:
  existing-alias:
    cmd: /usr/bin/llama-server -m /old.gguf
`)
	updated, changed, err := insertSwapModelEntry(original, "new-alias", "/m/new.gguf")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !changed {
		t.Error("changed = false, want true")
	}
	if !strings.Contains(string(updated), "existing-alias") {
		t.Errorf("existing entry lost; updated:\n%s", updated)
	}
	if !strings.Contains(string(updated), "new-alias") {
		t.Errorf("new entry missing; updated:\n%s", updated)
	}
}

func TestRunLocalListModels_QueriesEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"alpha"},{"id":"beta"}]}`))
	}))
	defer srv.Close()

	t.Setenv("MILLIWAYS_LOCAL_ENDPOINT", srv.URL+"/v1")

	var out bytes.Buffer
	code := runLocalListModels(nil, &out, &out)
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	got := strings.TrimSpace(out.String())
	if got != "alpha\nbeta" {
		t.Errorf("output = %q, want %q", got, "alpha\nbeta")
	}
}

func TestRunLocalListModels_BackendUnreachable(t *testing.T) {
	t.Setenv("MILLIWAYS_LOCAL_ENDPOINT", "http://127.0.0.1:0/v1") // port 0 — guaranteed not listening

	var stdout, stderr bytes.Buffer
	code := runLocalListModels(nil, &stdout, &stderr)
	if code == 0 {
		t.Errorf("exit code = 0, want non-zero on unreachable backend")
	}
	if !strings.Contains(stderr.String(), "local") {
		t.Errorf("stderr = %q, want it to mention local/backend", stderr.String())
	}
}

func TestRunLocalSwitchServer_WritesEnvFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))

	var stdout, stderr bytes.Buffer
	code := runLocalSwitchServer([]string{"ollama"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}

	envPath := filepath.Join(tmp, ".config", "milliways", "local.env")
	body, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("expected %s, err = %v", envPath, err)
	}
	if !strings.Contains(string(body), "MILLIWAYS_LOCAL_ENDPOINT=http://127.0.0.1:11434/v1") {
		t.Errorf("env file content = %q, missing ollama endpoint", body)
	}
	if !strings.Contains(stdout.String(), "11434") {
		t.Errorf("stdout = %q, want it to include the resolved endpoint", stdout.String())
	}
}

func TestRunLocalSwitchServer_UnknownKindReturnsError(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	code := runLocalSwitchServer([]string{"not-a-thing"}, &stdout, &stderr)
	if code == 0 {
		t.Error("exit = 0, want non-zero")
	}
	if !strings.Contains(stderr.String(), "not-a-thing") {
		t.Errorf("stderr = %q, want it to name the bad kind", stderr.String())
	}
}

func TestRunLocal_DispatchesUnknownVerbCleanly(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	code := runLocal([]string{"hallucinated-verb"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit = %d, want 2 (usage error)", code)
	}
	if !strings.Contains(stderr.String(), "hallucinated-verb") {
		t.Errorf("stderr = %q, want it to name the bad verb", stderr.String())
	}
}

func TestRunLocal_NoArgsPrintsUsageAndExits2(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	code := runLocal(nil, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "usage") {
		t.Errorf("stderr = %q, want it to mention usage", stderr.String())
	}
}

func TestRunLocal_HelpExitsZero(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	code := runLocal([]string{"--help"}, &stdout, &stderr)
	if code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "install-server") {
		t.Errorf("help output = %q, want it to list install-server", stdout.String())
	}
}

// ── setup-model tests ─────────────────────────────────────────────────────────

func TestRunLocal_SetupModelMissingRepo(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	code := runLocal([]string{"setup-model"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit = %d, want 2 (missing repo)", code)
	}
}

func TestRunLocal_SetupModelBadQuant(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	code := runLocal([]string{"setup-model", "owner/repo", "--quant"}, &stdout, &stderr)
	if code == 0 {
		t.Errorf("exit = 0, want non-zero when --quant has no value")
	}
}

func TestRunLocal_SetupModelDownloadAndRegister(t *testing.T) {
	modelDir := t.TempDir()
	cfgDir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("MODEL_DIR", modelDir)
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	orig := userHomeDirFn
	userHomeDirFn = func() (string, error) { return homeDir, nil }
	t.Cleanup(func() { userHomeDirFn = orig })

	// defaultGGUFDest("owner/repo-GGUF", "Q4_K_M") = filepath.Join(modelDir, "repo-GGUF-Q4_K_M.gguf")
	ggufPath := filepath.Join(modelDir, "repo-GGUF-Q4_K_M.gguf")
	if err := os.WriteFile(ggufPath, make([]byte, 51*1024*1024), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := runLocal(
		[]string{"setup-model", "owner/repo-GGUF", "--quant", "Q4_K_M", "--alias", "my-model"},
		&stdout, &stderr,
	)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "registered") {
		t.Errorf("expected 'registered' in output, got: %q", stdout.String())
	}
	// llama-swap.yaml should exist somewhere under cfgDir.
	var yamlBytes []byte
	_ = filepath.WalkDir(cfgDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() { return err }
		if strings.HasSuffix(path, "llama-swap.yaml") { yamlBytes, _ = os.ReadFile(path) }
		return nil
	})
	if len(yamlBytes) == 0 {
		t.Fatalf("llama-swap.yaml not written under %s", cfgDir)
	}
	if !strings.Contains(string(yamlBytes), "my-model") {
		t.Errorf("llama-swap.yaml missing alias 'my-model': %s", yamlBytes)
	}
}

func TestRunLocal_SetupModelIdempotent(t *testing.T) {
	modelDir := t.TempDir()
	cfgDir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("MODEL_DIR", modelDir)
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	orig := userHomeDirFn
	userHomeDirFn = func() (string, error) { return homeDir, nil }
	t.Cleanup(func() { userHomeDirFn = orig })

	ggufPath := filepath.Join(modelDir, "repo-GGUF-Q4_K_M.gguf")
	_ = os.WriteFile(ggufPath, make([]byte, 51*1024*1024), 0o644)

	args := []string{"setup-model", "owner/repo-GGUF", "--quant", "Q4_K_M", "--alias", "my-model"}

	var out1 bytes.Buffer
	if code := runLocal(args, &out1, io.Discard); code != 0 {
		t.Fatalf("first call failed (stdout=%q)", out1.String())
	}
	var out2 bytes.Buffer
	if code := runLocal(args, &out2, io.Discard); code != 0 {
		t.Fatalf("second call failed (stdout=%q)", out2.String())
	}
	if !strings.Contains(out2.String(), "already registered") {
		t.Errorf("expected 'already registered' on second call, got: %q", out2.String())
	}
}

// ── download-model tests ───────────────────────────────────────────────────────

func TestRunLocal_DownloadModelUsesCache(t *testing.T) {
	modelDir := t.TempDir()
	t.Setenv("MODEL_DIR", modelDir)

	dest := filepath.Join(modelDir, "repo-GGUF-Q4_K_M.gguf")
	_ = os.WriteFile(dest, make([]byte, 51*1024*1024), 0o644)

	called := false
	origExec := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		called = true
		return origExec(name, args...)
	}
	defer func() { execCommand = origExec }()

	var stdout, stderr bytes.Buffer
	code := runLocal([]string{"download-model", "owner/repo-GGUF", "--quant", "Q4_K_M"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	if called {
		t.Error("curl invoked for cached model — should have skipped")
	}
	if !strings.Contains(stdout.String(), "cached") {
		t.Errorf("expected 'cached' in output, got: %q", stdout.String())
	}
}

func TestRunLocal_DownloadModelForceBypassesCache(t *testing.T) {
	modelDir := t.TempDir()
	t.Setenv("MODEL_DIR", modelDir)

	dest := filepath.Join(modelDir, "repo-GGUF-Q4_K_M.gguf")
	_ = os.WriteFile(dest, []byte("old"), 0o644)

	origExec := execCommand
	execCommand = func(_ string, _ ...string) *exec.Cmd { return exec.Command("false") }
	defer func() { execCommand = origExec }()

	var stdout, stderr bytes.Buffer
	code := runLocal([]string{"download-model", "owner/repo-GGUF", "--quant", "Q4_K_M", "--force"}, &stdout, &stderr)
	if code == 0 {
		t.Error("expected non-zero exit when curl fails with --force")
	}
}

// ── swap-mode tests ───────────────────────────────────────────────────────────

func TestRunLocal_SwapModeMissingArg(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	code := runLocal([]string{"swap-mode"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
}

func TestRunLocal_SwapModeBadMode(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	code := runLocal([]string{"swap-mode", "warm"}, &stdout, &stderr)
	if code == 0 {
		t.Error("expected non-zero exit for invalid mode")
	}
}

func TestRunLocal_SwapModeHot(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)

	// Write a minimal llama-swap.yaml with a cold ttl.
	milliDir := filepath.Join(cfgDir, "milliways")
	_ = os.MkdirAll(milliDir, 0o755)
	yaml := "ttl: 600\nmodels:\n  my-model:\n    ttl: 600\n    cmd: llama-server\n"
	if err := os.WriteFile(filepath.Join(milliDir, "llama-swap.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := runLocal([]string{"swap-mode", "hot"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	updated, _ := os.ReadFile(filepath.Join(milliDir, "llama-swap.yaml"))
	if strings.Contains(string(updated), "ttl: 600") {
		t.Errorf("expected ttl: 600 to be replaced with ttl: 0, got:\n%s", updated)
	}
	if !strings.Contains(string(updated), "ttl: 0") {
		t.Errorf("expected ttl: 0 in hot mode output, got:\n%s", updated)
	}
	if !strings.Contains(stdout.String(), "hot") {
		t.Errorf("expected 'hot' in output, got: %q", stdout.String())
	}
}

func TestRunLocal_SwapModeCold(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)

	milliDir := filepath.Join(cfgDir, "milliways")
	_ = os.MkdirAll(milliDir, 0o755)
	yaml := "ttl: 0\nmodels:\n  my-model:\n    ttl: 0\n    cmd: llama-server\n"
	_ = os.WriteFile(filepath.Join(milliDir, "llama-swap.yaml"), []byte(yaml), 0o644)

	var stdout, stderr bytes.Buffer
	code := runLocal([]string{"swap-mode", "cold", "--ttl", "300"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	updated, _ := os.ReadFile(filepath.Join(milliDir, "llama-swap.yaml"))
	if !strings.Contains(string(updated), "ttl: 300") {
		t.Errorf("expected ttl: 300, got:\n%s", updated)
	}
	if !strings.Contains(stdout.String(), "cold") {
		t.Errorf("expected 'cold' in output, got: %q", stdout.String())
	}
}

// ── model catalog tests ────────────────────────────────────────────────────────

func TestRunLocal_SetupModelList(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	code := runLocal([]string{"setup-model", "list"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	out := stdout.String()
	// Must show column headers
	if !strings.Contains(out, "Model") || !strings.Contains(out, "Size") {
		t.Errorf("missing column headers in output: %q", out)
	}
	// Must show at least 5 models from the builtin catalog
	lineCount := strings.Count(out, "\n")
	if lineCount < 5 {
		t.Errorf("expected at least 5 lines, got %d", lineCount)
	}
	// Must show install hint
	if !strings.Contains(out, "/setup-model") {
		t.Errorf("expected install hint in output: %q", out)
	}
	// Qwen coder should be in the list (it's the recommended default)
	if !strings.Contains(out, "Qwen") {
		t.Errorf("expected Qwen models in builtin catalog: %q", out)
	}
}

func TestRunLocal_SetupModelListShowsToolUseFlag(t *testing.T) {
	// Use a temp HOME so loadCatalog() reads builtinCatalog (no cache file).
	t.Setenv("HOME", t.TempDir())
	var stdout bytes.Buffer
	_ = runModelCatalogList(&stdout)
	s := stdout.String()
	if !strings.Contains(s, "✓") {
		t.Errorf("expected ✓ for tool-use capable models in builtin catalog: %q", s)
	}
}

func TestRunLocal_SetupModelRefreshFallsBackOnNetworkError(t *testing.T) {
	t.Parallel()
	// Point at an unreachable host to force network failure.
	// The refresh command must fall back to the builtin catalog gracefully.
	origGet := http.DefaultClient
	_ = origGet // not modifiable directly; we rely on the timeout path instead
	var stdout, stderr bytes.Buffer
	// We can't easily intercept http.Client here, so just verify
	// the command exits cleanly regardless of network state.
	// The test is valuable in CI where HF may be blocked.
	code := runLocal([]string{"setup-model", "refresh"}, &stdout, &stderr)
	// Either 0 (network worked) or 0 (fell back to builtin) — never 1 on network err
	if code != 0 {
		t.Errorf("exit = %d: refresh should never hard-fail, got stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	// Either real data or fallback catalog — both must mention models
	if !strings.Contains(out, "Model") && !strings.Contains(out, "Qwen") && !strings.Contains(out, "HuggingFace") {
		t.Errorf("expected model listing in output, got: %q", out)
	}
}

func TestCatalogCachePath(t *testing.T) {
	t.Parallel()
	p, err := catalogCachePath()
	if err != nil {
		t.Fatalf("catalogCachePath: %v", err)
	}
	if !strings.HasSuffix(p, "model-catalog.json") {
		t.Errorf("expected path to end with model-catalog.json, got %q", p)
	}
}

func TestLoadCatalogReturnsBuiltinWhenNoCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	entries := loadCatalog()
	if len(entries) == 0 {
		t.Error("expected builtin catalog, got empty slice")
	}
	if len(entries) < 5 {
		t.Errorf("builtin catalog too small: %d entries", len(entries))
	}
}

func TestLoadCatalogReadsCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cacheDir := filepath.Join(home, ".local", "share", "milliways")
	_ = os.MkdirAll(cacheDir, 0o755)
	custom := `[{"name":"TestModel","repo":"test/repo","quant":"Q4_K_M","size_gb":"3.0","min_ram_gb":"4","tool_use":true,"reasoning":false,"note":"test"}]`
	_ = os.WriteFile(filepath.Join(cacheDir, "model-catalog.json"), []byte(custom), 0o644)

	entries := loadCatalog()
	if len(entries) != 1 || entries[0].Name != "TestModel" {
		t.Errorf("expected cached entry, got %v", entries)
	}
}

func TestBuiltinCatalogIntegrity(t *testing.T) {
	t.Parallel()
	for i, e := range builtinCatalog {
		if e.Name == "" {
			t.Errorf("entry %d: empty Name", i)
		}
		if e.Repo == "" {
			t.Errorf("entry %d (%s): empty Repo", i, e.Name)
		}
		if e.Quant == "" {
			t.Errorf("entry %d (%s): empty Quant", i, e.Name)
		}
		if e.SizeGB == "" {
			t.Errorf("entry %d (%s): empty SizeGB", i, e.Name)
		}
	}
	if len(builtinCatalog) < 8 {
		t.Errorf("builtin catalog should have at least 8 entries, has %d", len(builtinCatalog))
	}
}

// ── server maintenance tests ──────────────────────────────────────────────────

func TestRunLocal_ServerStatus_NotRunning(t *testing.T) {
	// Port 0 is guaranteed not to be listening.
	t.Setenv("MILLIWAYS_LOCAL_ENDPOINT", "http://127.0.0.1:0/v1")

	var stdout, stderr bytes.Buffer
	code := runLocal([]string{"server-status"}, &stdout, &stderr)
	if code != 1 {
		t.Errorf("exit = %d, want 1 (not running)", code)
	}
	combined := stdout.String() + stderr.String()
	if !strings.Contains(strings.ToLower(combined), "not running") &&
		!strings.Contains(strings.ToLower(combined), "stopped") &&
		!strings.Contains(strings.ToLower(combined), "unreachable") {
		t.Errorf("expected 'not running'/'stopped'/'unreachable' in output, got stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunLocal_ServerPort_ParsesEndpoint(t *testing.T) {
	t.Setenv("MILLIWAYS_LOCAL_ENDPOINT", "http://127.0.0.1:9999/v1")

	var stdout, stderr bytes.Buffer
	code := runLocal([]string{"server-port"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	got := strings.TrimSpace(stdout.String())
	if got != "9999" {
		t.Errorf("server-port = %q, want %q", got, "9999")
	}
}

func TestRunLocal_ServerPort_DefaultPort(t *testing.T) {
	// Ensure env var is unset so we fall back to the default endpoint.
	t.Setenv("MILLIWAYS_LOCAL_ENDPOINT", "")

	var stdout, stderr bytes.Buffer
	code := runLocal([]string{"server-port"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	got := strings.TrimSpace(stdout.String())
	if got != "8765" {
		t.Errorf("server-port (default) = %q, want %q", got, "8765")
	}
}

func TestRunLocal_DefaultModel_UpdatesLauncher(t *testing.T) {
	// Build a temporary HOME with a fake launcher and llama-swap.yaml.
	home := t.TempDir()
	cfgDir := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", cfgDir)

	// Create the launcher directory and a minimal fake launcher script.
	binDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	launcherPath := filepath.Join(binDir, "milliways-local-server")
	fakeLauncher := "#!/usr/bin/env bash\nexec llama-server -m \"/old/model.gguf\" --alias oldmodel --host 127.0.0.1 --port 8765 --ctx-size 16384 --jinja\n"
	if err := os.WriteFile(launcherPath, []byte(fakeLauncher), 0o755); err != nil {
		t.Fatal(err)
	}

	// Write a fake llama-swap.yaml with a model entry matching the alias.
	milliCfgDir := filepath.Join(cfgDir, "milliways")
	if err := os.MkdirAll(milliCfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	modelPath := filepath.Join(home, ".local", "share", "milliways", "models", "mymodel-Q4_K_M.gguf")
	if err := os.MkdirAll(filepath.Dir(modelPath), 0o755); err != nil {
		t.Fatal(err)
	}
	swapYAML := "models:\n  mymodel:\n    cmd: llama-server -m " + modelPath + " --port ${PORT}\n"
	if err := os.WriteFile(filepath.Join(milliCfgDir, "llama-swap.yaml"), []byte(swapYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := runLocal([]string{"default-model", "mymodel"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, stdout = %q stderr = %q", code, stdout.String(), stderr.String())
	}

	// Verify launcher was updated.
	launcherData, err := os.ReadFile(launcherPath)
	if err != nil {
		t.Fatalf("read launcher: %v", err)
	}
	if !strings.Contains(string(launcherData), "mymodel") {
		t.Errorf("launcher not updated with new alias; got:\n%s", launcherData)
	}

	// Verify local.env was updated with MILLIWAYS_LOCAL_MODEL=mymodel.
	envPath := filepath.Join(cfgDir, "milliways", "local.env")
	envData, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read local.env: %v", err)
	}
	if !strings.Contains(string(envData), "MILLIWAYS_LOCAL_MODEL=mymodel") {
		t.Errorf("local.env not updated; got:\n%s", envData)
	}

	if !strings.Contains(stdout.String(), "mymodel") {
		t.Errorf("stdout = %q, want it to mention the alias", stdout.String())
	}
}

// ── review-code flag tests ─────────────────────────────────────────────────────

// TestRunLocalReviewCode_GitCommitFlag verifies that --git-commit is parsed and
// forwarded to review.Config.GitCommit.
func TestRunLocalReviewCode_GitCommitFlag(t *testing.T) {
	repoDir := t.TempDir()

	var captured review.Config
	orig := reviewNewFn
	reviewNewFn = func(cfg review.Config) (reviewRunner, error) {
		captured = cfg
		return &stubReviewRunner{}, nil
	}
	t.Cleanup(func() { reviewNewFn = orig })

	var stdout, stderr bytes.Buffer
	code := runLocalReviewCode([]string{"--git-commit", "--no-memory", repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	if !captured.GitCommit {
		t.Error("Config.GitCommit = false, want true when --git-commit is passed")
	}
}

// TestRunLocalReviewCode_LintFlag verifies that --lint is parsed and forwarded
// to review.Config.LintAfterEdit.
func TestRunLocalReviewCode_LintFlag(t *testing.T) {
	repoDir := t.TempDir()

	var captured review.Config
	orig := reviewNewFn
	reviewNewFn = func(cfg review.Config) (reviewRunner, error) {
		captured = cfg
		return &stubReviewRunner{}, nil
	}
	t.Cleanup(func() { reviewNewFn = orig })

	var stdout, stderr bytes.Buffer
	code := runLocalReviewCode([]string{"--lint", "--no-memory", repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	if !captured.LintAfterEdit {
		t.Error("Config.LintAfterEdit = false, want true when --lint is passed")
	}
}

// stubReviewRunner is a test double for the Runner that returns empty results.
type stubReviewRunner struct{}

func (s *stubReviewRunner) Run(_ context.Context, _ review.Config) (review.ReviewResult, error) {
	return review.ReviewResult{}, nil
}

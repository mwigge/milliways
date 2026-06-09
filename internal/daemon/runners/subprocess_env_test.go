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

package runners

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestEnsureRunnerSystemPathAddsBashPath(t *testing.T) {
	path := ensureRunnerSystemPath("/tmp/copilot-bin")

	if !pathContains(path, "/tmp/copilot-bin") {
		t.Fatalf("custom path missing from %q", path)
	}
	if !pathContains(path, "/bin") {
		t.Fatalf("/bin missing from %q", path)
	}
	if !pathContains(path, "/usr/bin") {
		t.Fatalf("/usr/bin missing from %q", path)
	}
}

func TestSafeRunnerEnvMILLIWAYSPathKeepsSystemFallbacks(t *testing.T) {
	t.Setenv("PATH", "/should/not/win")
	t.Setenv("MILLIWAYS_PATH", "/tmp/copilot-bin")

	env := safeRunnerEnv()
	path := envValue(env, "PATH")
	if !pathContains(path, "/tmp/copilot-bin") {
		t.Fatalf("MILLIWAYS_PATH missing from PATH=%q", path)
	}
	if pathContains(path, "/should/not/win") {
		t.Fatalf("inherited PATH leaked despite MILLIWAYS_PATH override: %q", path)
	}
	if !pathContains(path, "/bin") {
		t.Fatalf("/bin missing from PATH=%q", path)
	}
}

func TestControlledRunnerEnvAddsIdentityWorkspaceAndShimPath(t *testing.T) {
	root := t.TempDir()
	shimDir := filepath.Join(root, "shims")
	binDir := filepath.Join(root, "bin")
	t.Setenv("PATH", binDir)
	t.Setenv("MILLIWAYS_PATH", "")

	env := controlledRunnerEnv(controlledRunnerEnvOptions{
		ClientID:  AgentIDCodex,
		SessionID: "session-1",
		Workspace: filepath.Join(root, "workspace"),
		ShimDir:   shimDir,
	})

	if got := envValue(env, "MILLIWAYS_CLIENT_ID"); got != AgentIDCodex {
		t.Fatalf("MILLIWAYS_CLIENT_ID = %q, want %q", got, AgentIDCodex)
	}
	if got := envValue(env, "MILLIWAYS_SESSION_ID"); got != "session-1" {
		t.Fatalf("MILLIWAYS_SESSION_ID = %q, want session-1", got)
	}
	if got := envValue(env, "MILLIWAYS_WORKSPACE_ROOT"); got == "" {
		t.Fatalf("MILLIWAYS_WORKSPACE_ROOT missing from env")
	}
	if got := envValue(env, "MILLIWAYS_SHIM_DIR"); got != shimDir {
		t.Fatalf("MILLIWAYS_SHIM_DIR = %q, want %q", got, shimDir)
	}
	if got := envValue(env, "MILLIWAYS_SHIMS_ENABLED"); got != "1" {
		t.Fatalf("MILLIWAYS_SHIMS_ENABLED = %q, want 1", got)
	}
	path := envValue(env, "PATH")
	if firstPath(path) != shimDir {
		t.Fatalf("PATH first entry = %q, want shim dir; PATH=%q", firstPath(path), path)
	}
	if !pathContains(path, binDir) {
		t.Fatalf("original PATH dir missing after shim prepend: %q", path)
	}
}

func TestControlledRunnerEnvMILLIWAYSPathStillOverridesInheritedPath(t *testing.T) {
	root := t.TempDir()
	shimDir := filepath.Join(root, "shims")
	milliwaysPath := filepath.Join(root, "milliways-bin")
	t.Setenv("PATH", filepath.Join(root, "inherited"))
	t.Setenv("MILLIWAYS_PATH", milliwaysPath)

	env := controlledRunnerEnv(controlledRunnerEnvOptions{
		ClientID:  AgentIDClaude,
		SessionID: "session-2",
		ShimDir:   shimDir,
	})
	path := envValue(env, "PATH")
	if firstPath(path) != shimDir {
		t.Fatalf("PATH first entry = %q, want shim dir; PATH=%q", firstPath(path), path)
	}
	if !pathContains(path, milliwaysPath) {
		t.Fatalf("MILLIWAYS_PATH missing from PATH=%q", path)
	}
	if pathContains(path, filepath.Join(root, "inherited")) {
		t.Fatalf("inherited PATH leaked despite MILLIWAYS_PATH override: %q", path)
	}
}

func TestControlledRunnerEnvExcludesHTTPProviderSecrets(t *testing.T) {
	for _, key := range []string{
		"MINIMAX_API_KEY",
		"KIMI_API_KEY",
		"MOONSHOT_API_KEY",
		"DEEPSEEK_API_KEY",
		"MILLIWAYS_LOCAL_API_KEY",
		"AWS_SECRET_ACCESS_KEY",
		"GITHUB_TOKEN",
		"GH_TOKEN",
	} {
		t.Setenv(key, "secret")
	}

	env := controlledRunnerEnv(controlledRunnerEnvOptions{ClientID: AgentIDCodex})
	for _, key := range []string{
		"MINIMAX_API_KEY",
		"KIMI_API_KEY",
		"MOONSHOT_API_KEY",
		"DEEPSEEK_API_KEY",
		"MILLIWAYS_LOCAL_API_KEY",
		"AWS_SECRET_ACCESS_KEY",
		"GITHUB_TOKEN",
		"GH_TOKEN",
	} {
		if got := envValue(env, key); got != "" {
			t.Fatalf("%s leaked into controlled runner env as %q", key, got)
		}
	}
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}

func firstPath(path string) string {
	parts := strings.Split(path, string(os.PathListSeparator))
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

func pathContains(path, want string) bool {
	for _, part := range strings.Split(path, string(os.PathListSeparator)) {
		if part == want {
			return true
		}
	}
	return false
}

// --- Task 1.2: OTel passthrough keys ---

func TestOTelEnvKeysPassThrough(t *testing.T) {
	otelKeys := []string{
		"CLAUDE_CODE_ENABLE_TELEMETRY",
		"CLAUDE_CODE_ENHANCED_TELEMETRY_BETA",
		"OTEL_TRACES_EXPORTER",
		"OTEL_METRICS_EXPORTER",
		"OTEL_LOGS_EXPORTER",
		"OTEL_EXPORTER_OTLP_PROTOCOL",
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_SERVICE_NAME",
		"OTEL_RESOURCE_ATTRIBUTES",
		"OTEL_METRIC_EXPORT_INTERVAL",
		"OTEL_LOGS_EXPORT_INTERVAL",
		"OTEL_TRACES_EXPORT_INTERVAL",
		"TRACEPARENT",
		"TRACESTATE",
	}
	for _, key := range otelKeys {
		t.Setenv(key, "test-value-"+key)
	}

	env := safeRunnerEnv()
	for _, key := range otelKeys {
		if got := envValue(env, key); got != "test-value-"+key {
			t.Errorf("%s = %q, want %q", key, got, "test-value-"+key)
		}
	}
}

func TestCredentialKeysStillExcluded(t *testing.T) {
	secrets := []string{"GITHUB_TOKEN", "GH_TOKEN", "AWS_SECRET_ACCESS_KEY", "MINIMAX_API_KEY"}
	for _, k := range secrets {
		t.Setenv(k, "should-not-leak")
	}
	env := safeRunnerEnv()
	for _, k := range secrets {
		if got := envValue(env, k); got != "" {
			t.Errorf("%s leaked into runner env as %q", k, got)
		}
	}
}

// --- Task 2.5: TRACEPARENT injection ---

func newTestSpanContext(t *testing.T) context.Context {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	tracer := tp.Tracer("test")
	ctx, _ := tracer.Start(context.Background(), "test-span")
	return ctx
}

func TestTraceParentInjectedFromActiveSpan(t *testing.T) {
	ctx := newTestSpanContext(t)
	env := controlledRunnerEnv(controlledRunnerEnvOptions{
		ClientID: "claude",
		Ctx:      ctx,
	})
	tp := envValue(env, "TRACEPARENT")
	if tp == "" {
		t.Fatal("TRACEPARENT missing from env")
	}
	// W3C format: 00-{32hex}-{16hex}-{2hex}
	parts := strings.Split(tp, "-")
	if len(parts) != 4 {
		t.Fatalf("TRACEPARENT %q has %d parts, want 4", tp, len(parts))
	}
	if parts[0] != "00" {
		t.Errorf("TRACEPARENT version = %q, want 00", parts[0])
	}
	if len(parts[1]) != 32 {
		t.Errorf("TRACEPARENT traceID len = %d, want 32", len(parts[1]))
	}
	if len(parts[2]) != 16 {
		t.Errorf("TRACEPARENT spanID len = %d, want 16", len(parts[2]))
	}
}

func TestTraceParentNotInjectedWithoutSpan(t *testing.T) {
	env := controlledRunnerEnv(controlledRunnerEnvOptions{
		ClientID: "claude",
		Ctx:      context.Background(),
	})
	if tp := envValue(env, "TRACEPARENT"); tp != "" {
		t.Errorf("TRACEPARENT should be absent with no active span, got %q", tp)
	}
}

func TestTraceParentOverwritesInheritedValue(t *testing.T) {
	t.Setenv("TRACEPARENT", "00-ffffffffffffffffffffffffffffffff-ffffffffffffffff-01")
	ctx := newTestSpanContext(t)
	env := controlledRunnerEnv(controlledRunnerEnvOptions{
		ClientID: "claude",
		Ctx:      ctx,
	})
	tp := envValue(env, "TRACEPARENT")
	if tp == "" {
		t.Fatal("TRACEPARENT missing from env")
	}
	// Injected value must differ from the inherited sentinel.
	if tp == "00-ffffffffffffffffffffffffffffffff-ffffffffffffffff-01" {
		t.Error("TRACEPARENT was not overwritten; inherited value preserved")
	}
	// Must still be valid W3C format.
	if len(strings.Split(tp, "-")) != 4 {
		t.Errorf("injected TRACEPARENT %q is not valid W3C format", tp)
	}
}

// --- Task 3.7: Scope B telemetry config injection ---

func TestScopeBInjectsFullBundle(t *testing.T) {
	tel := TelemetryEnv{
		SignozEndpoint: "http://localhost:4317",
		AgentID:        "claude",
		SessionID:      "sess-1",
		Kitchen:        "claude",
	}
	env := controlledRunnerEnv(controlledRunnerEnvOptions{
		ClientID:  "claude",
		Telemetry: tel,
	})
	cases := map[string]string{
		"CLAUDE_CODE_ENABLE_TELEMETRY":  "1",
		"OTEL_TRACES_EXPORTER":          "otlp",
		"OTEL_METRICS_EXPORTER":         "otlp",
		"OTEL_LOGS_EXPORTER":            "otlp",
		"OTEL_EXPORTER_OTLP_PROTOCOL":   "http/protobuf",
		"OTEL_EXPORTER_OTLP_ENDPOINT":   "http://localhost:4317",
		"OTEL_SERVICE_NAME":             "milliways-claude",
	}
	for k, want := range cases {
		if got := envValue(env, k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	attrs := envValue(env, "OTEL_RESOURCE_ATTRIBUTES")
	if !strings.Contains(attrs, "milliways.kitchen=claude") {
		t.Errorf("OTEL_RESOURCE_ATTRIBUTES %q missing milliways.kitchen", attrs)
	}
	if !strings.Contains(attrs, "milliways.session_id=sess-1") {
		t.Errorf("OTEL_RESOURCE_ATTRIBUTES %q missing milliways.session_id", attrs)
	}
}

func TestScopeBEnhancedTracingAddsVar(t *testing.T) {
	tel := TelemetryEnv{SignozEndpoint: "http://localhost:4317", EnhancedTracing: true}
	env := controlledRunnerEnv(controlledRunnerEnvOptions{Telemetry: tel})
	if got := envValue(env, "CLAUDE_CODE_ENHANCED_TELEMETRY_BETA"); got != "1" {
		t.Errorf("CLAUDE_CODE_ENHANCED_TELEMETRY_BETA = %q, want 1", got)
	}
}

func TestScopeBEnhancedTracingAbsentWhenFalse(t *testing.T) {
	tel := TelemetryEnv{SignozEndpoint: "http://localhost:4317", EnhancedTracing: false}
	env := controlledRunnerEnv(controlledRunnerEnvOptions{Telemetry: tel})
	if got := envValue(env, "CLAUDE_CODE_ENHANCED_TELEMETRY_BETA"); got != "" {
		t.Errorf("CLAUDE_CODE_ENHANCED_TELEMETRY_BETA should be absent, got %q", got)
	}
}

func TestScopeBAbsentWhenNoEndpoint(t *testing.T) {
	env := controlledRunnerEnv(controlledRunnerEnvOptions{ClientID: "claude"})
	if got := envValue(env, "CLAUDE_CODE_ENABLE_TELEMETRY"); got != "" {
		t.Errorf("CLAUDE_CODE_ENABLE_TELEMETRY should be absent with no endpoint, got %q", got)
	}
}

func TestScopeBEndpointOverridesShell(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://other:4317")
	tel := TelemetryEnv{SignozEndpoint: "http://localhost:4317", AgentID: "codex"}
	env := controlledRunnerEnv(controlledRunnerEnvOptions{Telemetry: tel})
	if got := envValue(env, "OTEL_EXPORTER_OTLP_ENDPOINT"); got != "http://localhost:4317" {
		t.Errorf("OTEL_EXPORTER_OTLP_ENDPOINT = %q, want carte.yaml value", got)
	}
}

func readEnvCapture(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read env capture: %v", err)
	}
	fields := strings.Split(strings.TrimSpace(string(raw)), "\t")
	if len(fields) != 6 || fields[0] != "ENV" {
		t.Fatalf("bad env capture: %q", raw)
	}
	return fields
}

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
	"strings"
	"testing"

	"github.com/mwigge/milliways/internal/security/shims"
)

func envValue(env []string, key string) (string, bool) {
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return strings.TrimPrefix(e, prefix), true
		}
	}
	return "", false
}

// TestShimExecChildEnv_RestoresShimDir covers S1: a brokered exec must hand the
// real binary a PATH that still contains the shim directory, so nested
// bare-name commands (curl/npm/pip spawned by an approved bash) are re-brokered
// instead of bypassing the control plane.
func TestShimExecChildEnv_RestoresShimDir(t *testing.T) {
	const shimDir = "/home/user/.milliways/shims"
	// The generated shim script hands milliwaysctl a PATH with the shim dir
	// already stripped, but preserves the original (with shim dir) in
	// EnvOriginalPath.
	t.Setenv("PATH", "/usr/local/bin:/usr/bin")
	t.Setenv(shims.EnvOriginalPath, shimDir+":/usr/local/bin:/usr/bin")
	// Shim-control vars must be scrubbed from the child.
	t.Setenv(shims.EnvActive, "1")
	t.Setenv(shims.EnvResolvedPath, "/usr/bin/bash")

	env := shimExecChildEnv(nil)

	path, ok := envValue(env, "PATH")
	if !ok {
		t.Fatal("child env missing PATH")
	}
	if !strings.Contains(path, shimDir) {
		t.Fatalf("child PATH must restore shim dir; got %q", path)
	}
	if path != shimDir+":/usr/local/bin:/usr/bin" {
		t.Fatalf("child PATH = %q, want original path with shim dir", path)
	}
	// The shim-control env vars must not leak to the real binary.
	if _, present := envValue(env, shims.EnvActive); present {
		t.Errorf("child env leaked %s", shims.EnvActive)
	}
	if _, present := envValue(env, shims.EnvResolvedPath); present {
		t.Errorf("child env leaked %s", shims.EnvResolvedPath)
	}
	if _, present := envValue(env, shims.EnvOriginalPath); present {
		t.Errorf("child env leaked %s", shims.EnvOriginalPath)
	}
}

// TestShimExecChildEnv_DecisionOverride covers the daemon-supplied environment
// override path: a decision's environment map (including PATH) takes precedence
// and is applied to the child env.
func TestShimExecChildEnv_DecisionOverride(t *testing.T) {
	const shimDir = "/opt/milliways/shims"
	t.Setenv("PATH", "/usr/bin")
	t.Setenv(shims.EnvOriginalPath, shimDir+":/usr/bin")

	decisionEnv := map[string]string{
		"PATH":                   shimDir + ":/custom/bin",
		"MILLIWAYS_POLICY_TOKEN": "abc123",
	}
	env := shimExecChildEnv(decisionEnv)

	path, _ := envValue(env, "PATH")
	if path != shimDir+":/custom/bin" {
		t.Fatalf("decision PATH override not applied; got %q", path)
	}
	if !strings.Contains(path, shimDir) {
		t.Fatalf("child PATH must retain shim dir; got %q", path)
	}
	if v, _ := envValue(env, "MILLIWAYS_POLICY_TOKEN"); v != "abc123" {
		t.Fatalf("decision env var not applied; got %q", v)
	}
}

// TestRenderSecurityClientEnforcement_ConveysMechanism covers S2 (reporting):
// the status line must make the enforcement MECHANISM explicit so a strict-mode
// user is not misled — in-process firewall for http/local runners vs. PATH-shim
// brokerage for subprocess CLI runners.
func TestRenderSecurityClientEnforcement_ConveysMechanism(t *testing.T) {
	clients := map[string]any{
		"minimax": map[string]any{"level": "full"},
		"claude": map[string]any{
			"level":          "brokered",
			"controlled_env": true,
			"broker_path":    "/home/user/.milliways/shims/claude",
		},
		"codex": map[string]any{"level": "preflight-only", "controlled_env": true},
	}
	out := renderSecurityClientEnforcement(clients, true /*shimsReady*/, true /*hasShims*/)

	if !strings.Contains(out, "in-process firewall") {
		t.Errorf("full runner must be labeled in-process firewall; got %q", out)
	}
	if !strings.Contains(out, "via PATH shim") {
		t.Errorf("brokered subprocess runner must be labeled PATH shim; got %q", out)
	}
	// The http/local runner must not be labeled as PATH-shim enforced.
	var minimaxSeg string
	for _, seg := range strings.Split(out, "; ") {
		if strings.HasPrefix(seg, "minimax ") {
			minimaxSeg = seg
		}
	}
	if minimaxSeg == "" {
		t.Fatalf("minimax segment not found in %q", out)
	}
	if strings.Contains(minimaxSeg, "via PATH shim") {
		t.Errorf("in-process runner must not claim PATH-shim enforcement; got %q", minimaxSeg)
	}
}

// TestDecisionEnvironment_ParsesStringMap ensures the decision "environment"
// field is parsed defensively (only string values survive).
func TestDecisionEnvironment_ParsesStringMap(t *testing.T) {
	decision := map[string]any{
		"action": "allow",
		"environment": map[string]any{
			"PATH":   "/a:/b",
			"IGNORE": 42, // non-string dropped
		},
	}
	got := decisionEnvironment(decision)
	if got["PATH"] != "/a:/b" {
		t.Fatalf("PATH = %q, want /a:/b", got["PATH"])
	}
	if _, ok := got["IGNORE"]; ok {
		t.Fatalf("non-string decision env value should be dropped")
	}

	if decisionEnvironment(map[string]any{"action": "allow"}) != nil {
		t.Fatalf("missing environment should yield nil map")
	}
}

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

// claudeLocalCoderMCP wires a milliwaysctl mcp-localcoder subprocess into
// each Claude invocation when a local LLM server is reachable. Claude
// receives a --mcp-config pointing at the config file and an
// --append-system-prompt explaining the local_code tool.
//
// The config is computed once per process (sync.Once) since the local
// endpoint does not change at runtime. The temp file is never deleted
// during normal operation; the OS reclaims it on reboot.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

var (
	lcOnce      sync.Once
	lcMCPPath   string // path to temp MCP JSON config, "" = not available
	lcSysPrompt string // --append-system-prompt text
)

// claudeLocalCoderArgs returns additional Claude CLI args needed to register
// the local_code MCP tool. Returns nil when the local server is unreachable
// or milliwaysctl is not on PATH.
func claudeLocalCoderArgs() []string {
	lcOnce.Do(initLocalCoderMCP)
	if lcMCPPath == "" {
		return nil
	}
	return []string{
		"--mcp-config", lcMCPPath,
		"--append-system-prompt", lcSysPrompt,
	}
}

func initLocalCoderMCP() {
	endpoint := strings.TrimRight(os.Getenv("MILLIWAYS_LOCAL_ENDPOINT"), "/")
	if endpoint == "" {
		endpoint = "http://127.0.0.1:8765/v1"
	}
	model := os.Getenv("MILLIWAYS_LOCAL_MODEL")
	if model == "" {
		model = "default"
	}

	if !localCoderServerReachable(endpoint) {
		return
	}

	ctlBin, err := exec.LookPath("milliwaysctl")
	if err != nil {
		return
	}

	cfgEnv := map[string]string{
		"MILLIWAYS_LOCAL_ENDPOINT": endpoint,
		"MILLIWAYS_LOCAL_MODEL":    model,
	}
	if key := os.Getenv("MILLIWAYS_LOCAL_API_KEY"); key != "" {
		cfgEnv["MILLIWAYS_LOCAL_API_KEY"] = key
	}

	cfgJSON, err := json.Marshal(map[string]any{
		"mcpServers": map[string]any{
			"local_coder": map[string]any{
				"command": ctlBin,
				"args":    []string{"mcp-localcoder"},
				"env":     cfgEnv,
			},
		},
	})
	if err != nil {
		return
	}

	f, err := os.CreateTemp("", "milliways-mcp-localcoder-*.json")
	if err != nil {
		return
	}
	if _, err := f.Write(cfgJSON); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return
	}
	_ = f.Close()

	lcMCPPath = f.Name()
	lcSysPrompt = fmt.Sprintf(
		"A local on-device AI coding server is available as the 'local_code' MCP tool "+
			"(endpoint: %s, model: %s). "+
			"Use it to delegate fast implementation tasks: write functions, generate boilerplate, "+
			"produce tests, or implement well-specified specs. "+
			"Coordinate and plan with your own intelligence; let local_code handle code generation. "+
			"If local_code takes more than 15 seconds or returns poor-quality output, "+
			"fall back to solving the task yourself.",
		endpoint, model,
	)
}

func localCoderServerReachable(endpoint string) bool {
	hc := &http.Client{Timeout: 2 * time.Second}
	resp, err := hc.Get(endpoint + "/models")
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

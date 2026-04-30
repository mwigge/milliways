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

package maitre

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mwigge/milliways/internal/kitchen"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

// KitchenHealth summarizes a kitchen's readiness.
type KitchenHealth struct {
	Name       string
	Status     kitchen.Status
	CostTier   kitchen.CostTier
	InstallCmd string
	AuthCmd    string
}

// Diagnose checks all kitchens and returns their health.
func Diagnose(reg *kitchen.Registry) []KitchenHealth {
	var results []KitchenHealth
	for name, k := range reg.All() {
		h := KitchenHealth{
			Name:     name,
			Status:   k.Status(),
			CostTier: k.CostTier(),
		}
		if s, ok := k.(kitchen.Setupable); ok {
			h.InstallCmd = s.InstallCmd()
			h.AuthCmd = s.AuthCmd()
		}
		results = append(results, h)
	}
	return results
}

// ReadyCounts returns (ready, total) kitchen counts.
func ReadyCounts(health []KitchenHealth) (int, int) {
	ready := 0
	for _, h := range health {
		if h.Status == kitchen.Ready {
			ready++
		}
	}
	return ready, len(health)
}

// PrintStatus renders the kitchen status table to stdout.
func PrintStatus(health []KitchenHealth) {
	fmt.Println("Kitchen      Status              Cost    Action")
	fmt.Println("───────      ──────              ────    ──────")

	for _, h := range health {
		action := ""
		switch h.Status {
		case kitchen.NotInstalled:
			action = h.InstallCmd
		case kitchen.NeedsAuth:
			action = h.AuthCmd
		case kitchen.Disabled:
			action = "(disabled in carte.yaml)"
		}

		fmt.Printf("%-12s %s %-18s %-7s %s\n",
			h.Name,
			h.Status.Symbol(),
			h.Status,
			h.CostTier,
			action,
		)
	}

	ready, total := ReadyCounts(health)
	fmt.Printf("\n%d/%d kitchens ready.", ready, total)
	if ready < total {
		fmt.Print(" Run 'milliways --setup <kitchen>' to add more.")
	}
	fmt.Println()
}

// SetupKitchen attempts to install and/or authenticate a kitchen.
// Returns nil on success, error on failure. The kitchen must implement
// Setupable; otherwise setup is not supported.
func SetupKitchen(k kitchen.Kitchen) error {
	s, ok := k.(kitchen.Setupable)
	if !ok {
		return fmt.Errorf("%s does not support setup", k.Name())
	}

	status := k.Status()

	switch status {
	case kitchen.Disabled:
		return fmt.Errorf("%s is disabled in carte.yaml — set enabled: true to use it", k.Name())
	case kitchen.Ready:
		fmt.Printf("✓ %s is already ready.\n", k.Name())
		return nil
	case kitchen.NotInstalled:
		fmt.Printf("Installing %s...\n", k.Name())
		installCmd := s.InstallCmd()
		if installCmd == "" {
			return fmt.Errorf("no install command configured for %s", k.Name())
		}
		parts := strings.Fields(installCmd)
		cmd := exec.Command(parts[0], parts[1:]...)
		cmd.Stdout = nil
		cmd.Stderr = nil
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("installing %s: %w\n  Try manually: %s", k.Name(), err, installCmd)
		}
		fmt.Printf("✓ %s installed.\n", k.Name())

		// Re-check — might need auth now
		if k.Status() == kitchen.Ready {
			fmt.Printf("✓ %s kitchen ready.\n", k.Name())
			return nil
		}
		fmt.Printf("  %s installed but needs authentication.\n", k.Name())
		fallthrough

	case kitchen.NeedsAuth:
		authCmd := s.AuthCmd()
		if authCmd == "" {
			return fmt.Errorf("no auth command configured for %s", k.Name())
		}
		fmt.Printf("Authenticate %s:\n  $ %s\n", k.Name(), authCmd)
		return fmt.Errorf("run the auth command above, then retry 'milliways status'")
	}

	return fmt.Errorf("unknown status for %s: %s", k.Name(), status)
}

func isTTY() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// UpdateKitchenAuth updates the named kitchen's HTTP auth key in carte.yaml.
func UpdateKitchenAuth(kitchenName, apiKey string) error {
	configPath := DefaultConfigPath()
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("reading config %s: %w", configPath, err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("parsing config %s: %w", configPath, err)
	}

	document := &root
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		document = root.Content[0]
	}

	kitchensNode, ok := yamlMapValue(document, "kitchens")
	if !ok {
		return fmt.Errorf("config %s does not define kitchens", configPath)
	}

	kitchenNode, ok := yamlMapValue(kitchensNode, kitchenName)
	if !ok {
		return fmt.Errorf("kitchen %q not found in %s", kitchenName, configPath)
	}

	httpClientNode, ok := yamlMapValue(kitchenNode, "http_client")
	if !ok {
		return fmt.Errorf("kitchen %q is not an HTTPClient type", kitchenName)
	}

	setYAMLMapValue(httpClientNode, "auth_key", strings.TrimSpace(apiKey))

	backupPath := configPath + ".bak"
	if err := os.WriteFile(backupPath, data, 0o600); err != nil {
		return fmt.Errorf("writing backup %s: %w", backupPath, err)
	}

	updated, err := yaml.Marshal(&root)
	if err != nil {
		return fmt.Errorf("encoding updated config %s: %w", configPath, err)
	}

	tempFile, err := os.CreateTemp(filepath.Dir(configPath), filepath.Base(configPath)+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp config for %s: %w", configPath, err)
	}
	tempPath := tempFile.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()

	if _, err := tempFile.Write(updated); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("writing temp config for %s: %w", configPath, err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("closing temp config for %s: %w", configPath, err)
	}

	if err := os.Rename(tempPath, configPath); err != nil {
		return fmt.Errorf("replacing config %s: %w", configPath, err)
	}

	return nil
}

// LoginKitchen starts the appropriate authentication flow for a kitchen.
func LoginKitchen(kitchenName string) error {
	switch kitchenName {
	case "claude":
		return loginCLIOAuth("claude", "claude", "auth", "login")
	case "gemini":
		return loginCLIOAuth("gemini", "gemini", "auth", "login")
	case "opencode":
		return loginInteractiveTUI("opencode", "opencode", "providers")
	case "minimax":
		return LoginAPIKey("minimax")
	case "groq":
		return loginEnvVar("groq", "GROQ_API_KEY", "https://console.groq.com/keys")
	case "ollama":
		return loginOllama()
	case "aider":
		return loginEnvVar("aider", "ANTHROPIC_API_KEY", "https://aider.chat/docs/usage.html")
	case "goose":
		return loginEnvVar("goose", "GOOSE_API_KEY", "https://github.com/gooseai/goose")
	case "cline":
		return loginEnvVar("cline", "ANTHROPIC_API_KEY", "https://github.com/cline/cline")
	default:
		return fmt.Errorf("unknown kitchen: %s", kitchenName)
	}
}

func loginCLIOAuth(name, cli string, subcmd ...string) error {
	args := strings.Join(append([]string{cli}, subcmd...), " ")
	fmt.Printf("Running %s login: %s\n", name, args)

	cmd := exec.Command(cli, subcmd...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("running %s login command %q: %w", name, args, err)
	}

	return nil
}

func loginInteractiveTUI(name, cli string, args ...string) error {
	cmd := exec.Command(cli, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("running %s interactive login: %w", name, err)
	}

	return nil
}

func LoginAPIKey(name string) error {
	if !isTTY() {
		fmt.Printf("%s login requires an interactive terminal. Re-run this command in a TTY to enter your API key.\n", name)
		return nil
	}

	fmt.Print("Enter your API key: ")
	secret, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return fmt.Errorf("reading %s API key: %w", name, err)
	}

	key := strings.TrimSpace(string(secret))
	if key == "" {
		return fmt.Errorf("%s API key cannot be empty", name)
	}

	if err := UpdateKitchenAuth(name, key); err != nil {
		return fmt.Errorf("updating %s auth: %w", name, err)
	}

	return nil
}

func loginEnvVar(name, envVar, docsURL string) error {
	fmt.Printf("%s uses the %s environment variable.\n  Set it in your shell profile:\n    export %s=your_key_here\n  Get your key at: %s\n", name, envVar, envVar, docsURL)
	return nil
}

func loginOllama() error {
	fmt.Println("Ollama uses no authentication. Verifying service...")

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://localhost:11434")
	if err != nil {
		fmt.Println("✗ Ollama is not running at localhost:11434. Start it with: ollama serve")
		return nil
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	fmt.Println("✓ Ollama is running")
	return nil
}

func yamlMapValue(node *yaml.Node, key string) (*yaml.Node, bool) {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil, false
	}

	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1], true
		}
	}

	return nil, false
}

func setYAMLMapValue(node *yaml.Node, key, value string) {
	if node.Kind != yaml.MappingNode {
		node.Kind = yaml.MappingNode
		node.Tag = "!!map"
		node.Content = nil
	}

	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content[i+1].Kind = yaml.ScalarNode
			node.Content[i+1].Tag = "!!str"
			node.Content[i+1].Value = value
			return
		}
	}

	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value},
	)
}

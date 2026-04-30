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

package tools

// Security guardrails for tools the model can invoke from an agentic
// loop. The agentic tool loop ships RCE-shaped primitives (Bash, file
// Write/Edit, WebFetch) and exposes them to remote model output that may
// be adversarial — directly via prompt injection, or indirectly via
// tool-output fold-back. This file is the single source of truth for
// the constraints applied before any handler touches the host.
//
// Container model:
//   - Workspace root (MILLIWAYS_WORKSPACE_ROOT) — file Read/Write/Edit/
//     Grep/Glob are jailed inside this directory. Default = process cwd.
//   - Dotfile denylist — even inside the workspace, reads of
//     ~/.ssh/, ~/.aws/, ~/.gnupg/, ~/.kube/, ~/.docker/config.json,
//     ~/.netrc, ~/.config/milliways/local.env are refused.
//   - WebFetch — http(s) only; loopback / RFC1918 / link-local / cloud-
//     metadata IPs are rejected pre-resolve and on every redirect.
//   - Bash — cwd pinned to workspace root; command string dropped from
//     logs to avoid leaking model-generated secrets.

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)


// WorkspaceRoot returns the absolute path of the directory tools may
// touch via Read/Write/Edit/Grep/Glob. Resolves $MILLIWAYS_WORKSPACE_ROOT
// if set; otherwise defaults to process cwd. Empty string on error so
// callers can fail closed.
func WorkspaceRoot() string {
	if v := strings.TrimSpace(os.Getenv("MILLIWAYS_WORKSPACE_ROOT")); v != "" {
		abs, err := filepath.Abs(v)
		if err != nil {
			return ""
		}
		return abs
	}
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return cwd
}

// dotfileDenylist names path components / suffixes that file tools refuse
// to touch even when they fall inside the workspace root. Selected to
// cover the most common credential locations a malicious prompt-injection
// would target.
var dotfileDenylist = []string{
	".ssh",
	".aws",
	".gnupg",
	".gpg",
	".kube",
	".netrc",
	".docker/config.json",
	".config/milliways/local.env",
	".config/gh/hosts.yml",
	".config/anthropic/auth.json",
}

// containedPath validates that `path` resolves to a location inside the
// workspace root and does not match the dotfile denylist. Returns the
// resolved absolute path on success, or an error describing the refusal.
//
// Symlink resolution: this function uses filepath.Abs (which does not
// resolve symlinks). For the threat model, this is intentional —
// EvalSymlinks would require the path to exist, which fails for newly
// created files. The trade-off is that a symlink inside the workspace
// pointing outside it can escape; users who care about that should
// configure the workspace root to a directory under their control and
// audit symlinks within it.
func containedPath(path string) (string, error) {
	if path == "" {
		return "", errors.New("path is required")
	}
	root := WorkspaceRoot()
	if root == "" {
		return "", errors.New("workspace root unresolvable; refusing tool access")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", path, err)
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || strings.HasPrefix(rel, "..") || rel == ".." {
		return "", fmt.Errorf("path %q outside workspace root %q (set MILLIWAYS_WORKSPACE_ROOT to widen)", path, root)
	}
	if denied := matchDenylist(abs); denied != "" {
		return "", fmt.Errorf("path %q matches credential denylist %q", path, denied)
	}
	return abs, nil
}

// matchDenylist returns the first dotfile-denylist entry that matches abs,
// or "" if none. Uses path-component matching so e.g. `.ssh` matches both
// `/home/u/.ssh/known_hosts` and `/home/u/project/.ssh/secret`.
func matchDenylist(abs string) string {
	abs = filepath.Clean(abs)
	for _, deny := range dotfileDenylist {
		if strings.Contains(deny, "/") {
			// Multi-segment denylist entry (e.g. ".docker/config.json").
			if strings.HasSuffix(abs, string(filepath.Separator)+filepath.FromSlash(deny)) {
				return deny
			}
			continue
		}
		// Single-segment entry (e.g. ".ssh"). Match it as any path component.
		parts := strings.Split(abs, string(filepath.Separator))
		for _, p := range parts {
			if p == deny {
				return deny
			}
		}
	}
	return ""
}

// safeURL validates a URL for the WebFetch tool. Rejects:
//   - non-http(s) schemes (file://, gopher://, ftp://, etc.)
//   - hosts that resolve to loopback (127.0.0.0/8, ::1)
//   - hosts that resolve to link-local (169.254.0.0/16, fe80::/10)
//   - hosts that resolve to RFC1918 (10/8, 172.16/12, 192.168/16)
//   - cloud metadata hostnames (metadata.google.internal,
//     169.254.169.254)
//
// Returns the parsed URL on success.
func safeURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("scheme %q not allowed (only http/https)", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return nil, fmt.Errorf("url missing host")
	}
	lower := strings.ToLower(host)
	if lower == "metadata.google.internal" || lower == "metadata" {
		return nil, fmt.Errorf("host %q matches cloud-metadata block", host)
	}
	// Resolve and check every IP. Fail if any address is in a blocked range.
	addrs, err := net.LookupIP(host)
	if err != nil {
		return nil, fmt.Errorf("resolve host %q: %w", host, err)
	}
	for _, ip := range addrs {
		if isBlockedIP(ip) {
			return nil, fmt.Errorf("host %q resolves to blocked address %s (loopback / link-local / RFC1918 / cloud-metadata)", host, ip)
		}
	}
	return u, nil
}

// isBlockedIP returns true if ip is in a range we refuse to fetch from
// for SSRF reasons. Includes IPv4 and IPv6 equivalents where defined.
//
// MILLIWAYS_TOOLS_ALLOW_LOOPBACK=1 bypasses the loopback / link-local
// blocks. Intended for local development against locally-hosted backends
// (httptest fixtures, local proxies, dev mocks). Off by default in
// production. The metadata-IP check (169.254.x) is unconditional —
// allowing loopback does NOT allow cloud metadata.
func isBlockedIP(ip net.IP) bool {
	allowLoopback := os.Getenv("MILLIWAYS_TOOLS_ALLOW_LOOPBACK") == "1"
	if !allowLoopback {
		if ip.IsLoopback() {
			return true
		}
		if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return true
		}
		if ip.IsPrivate() {
			return true
		}
		if ip.IsUnspecified() {
			return true
		}
	}
	// Cloud metadata IPv4 (some providers): 169.254.169.254 already covered
	// by IsLinkLocalUnicast above when loopback is NOT allowed. When
	// loopback IS allowed we still block 169.254.x to keep the cloud-
	// metadata escape closed.
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 169 && ip4[1] == 254 {
			return true
		}
	}
	return false
}

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

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const defaultReadLimit = 256 * 1024

// handleRead returns file contents up to the configured limit. Refuses
// reads outside the workspace root or matching the credential denylist.
func handleRead(_ context.Context, args map[string]any) (string, error) {
	rawPath, ok := pathArg(args)
	if !ok {
		return "", errors.New("path is required")
	}
	path, err := containedPath(rawPath)
	if err != nil {
		return "", err
	}
	limit := intArg(args, "limit", defaultReadLimit)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read file %q: %w", path, err)
	}
	if len(data) > limit {
		data = data[:limit]
	}
	return string(data), nil
}

// handleWrite writes file contents and creates a backup for existing files.
// Refuses writes outside the workspace root or matching the credential
// denylist (~/.ssh/, ~/.aws/, etc.).
func handleWrite(_ context.Context, args map[string]any) (string, error) {
	rawPath, ok := pathArg(args)
	if !ok {
		return "", errors.New("path is required")
	}
	path, err := containedPath(rawPath)
	if err != nil {
		return "", err
	}
	content, ok := stringArg(args, "content")
	if !ok {
		return "", errors.New("content is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create parent dir for %q: %w", path, err)
	}
	if existing, err := os.ReadFile(path); err == nil {
		if err := os.WriteFile(path+".bak", existing, 0o600); err != nil {
			return "", fmt.Errorf("write backup for %q: %w", path, err)
		}
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return "", fmt.Errorf("write file %q: %w", path, err)
	}
	return "ok", nil
}

// handleEdit applies a minimal unified diff to a file. Refuses edits
// outside the workspace root or matching the credential denylist.
func handleEdit(_ context.Context, args map[string]any) (string, error) {
	rawPath, ok := pathArg(args)
	if !ok {
		return "", errors.New("path is required")
	}
	path, err := containedPath(rawPath)
	if err != nil {
		return "", err
	}
	diff, ok := stringArg(args, "diff")
	if !ok || strings.TrimSpace(diff) == "" {
		return "", errors.New("diff is required")
	}
	originalBytes, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read file %q: %w", path, err)
	}
	updated, err := applyUnifiedDiff(string(originalBytes), diff)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path+".bak", originalBytes, 0o600); err != nil {
		return "", fmt.Errorf("write backup for %q: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(updated), 0o600); err != nil {
		return "", fmt.Errorf("write edited file %q: %w", path, err)
	}
	return "ok", nil
}

// handleGrep searches files under the given path for regex matches.
func handleGrep(_ context.Context, args map[string]any) (string, error) {
	pattern, ok := stringArg(args, "pattern")
	if !ok {
		return "", errors.New("pattern is required")
	}
	rawRoot, _ := stringArg(args, "path")
	if strings.TrimSpace(rawRoot) == "" {
		rawRoot = "."
	}
	root, err := containedPath(rawRoot)
	if err != nil {
		return "", err
	}
	include, _ := stringArg(args, "include")
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("compile pattern: %w", err)
	}

	var matches []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if include != "" {
			matched, matchErr := filepath.Match(include, filepath.Base(path))
			if matchErr != nil {
				return matchErr
			}
			if !matched {
				return nil
			}
		}
		if matchDenylist(path) != "" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		lineNumber := 0
		for scanner.Scan() {
			lineNumber++
			line := scanner.Text()
			if re.MatchString(line) {
				matches = append(matches, fmt.Sprintf("%s:%d:%s", path, lineNumber, line))
			}
		}
		return scanner.Err()
	})
	if err != nil {
		return "", fmt.Errorf("grep files: %w", err)
	}
	sort.Strings(matches)
	return strings.Join(matches, "\n"), nil
}

// handleGlob returns filepath.Glob matches.
func handleGlob(_ context.Context, args map[string]any) (string, error) {
	pattern, ok := stringArg(args, "pattern")
	if !ok {
		return "", errors.New("pattern is required")
	}
	rawRoot, _ := stringArg(args, "path")
	if strings.TrimSpace(rawRoot) != "" {
		pattern = filepath.Join(rawRoot, pattern)
	}
	if _, err := containedPath(filepath.Dir(pattern)); err != nil {
		return "", fmt.Errorf("glob path outside workspace: %w", err)
	}
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", fmt.Errorf("glob %q: %w", pattern, err)
	}
	sort.Strings(matches)
	return strings.Join(matches, "\n"), nil
}

// handleWebFetch fetches a URL and returns the response body. Validates
// the URL against the SSRF allowlist (http(s) only, no loopback /
// link-local / RFC1918 / cloud-metadata) and re-validates the target on
// every redirect so a 302 to 169.254.169.254 cannot escape the check.
func handleWebFetch(ctx context.Context, args map[string]any) (string, error) {
	rawURL, ok := stringArg(args, "url")
	if !ok {
		return "", errors.New("url is required")
	}
	if _, err := safeURL(rawURL); err != nil {
		return "", fmt.Errorf("webfetch refused: %w", err)
	}
	timeout := durationArg(args, "timeout", 30*time.Second)
	client := &http.Client{
		Timeout: timeout,
		// Re-validate every redirect target — without this, an allowed
		// host could 302 us to 169.254.169.254 and the SSRF check is
		// bypassed.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			if _, err := safeURL(req.URL.String()); err != nil {
				return fmt.Errorf("redirect refused: %w", err)
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch url: %w", err)
	}
	defer resp.Body.Close()
	data, err := ioReadAllLimited(resp.Body, defaultReadLimit)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func applyUnifiedDiff(original, diff string) (string, error) {
	var removed []string
	var added []string
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "---"), strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "@@"):
			continue
		case strings.HasPrefix(line, "-"):
			removed = append(removed, strings.TrimPrefix(line, "-"))
		case strings.HasPrefix(line, "+"):
			added = append(added, strings.TrimPrefix(line, "+"))
		}
	}
	oldBlock := strings.Join(removed, "\n")
	newBlock := strings.Join(added, "\n")
	if oldBlock == "" {
		return "", errors.New("diff must remove at least one line")
	}
	if !strings.Contains(original, oldBlock) {
		return "", fmt.Errorf("diff target not found")
	}
	return strings.Replace(original, oldBlock, newBlock, 1), nil
}

func ioReadAllLimited(r io.Reader, limit int) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, int64(limit)))
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	return data, nil
}

func pathArg(args map[string]any) (string, bool) {
	if value, ok := stringArg(args, "path"); ok {
		return value, true
	}
	return stringArg(args, "file_path")
}

func stringArg(args map[string]any, key string) (string, bool) {
	value, ok := args[key].(string)
	if !ok {
		return "", false
	}
	return value, true
}

func intArg(args map[string]any, key string, fallback int) int {
	value, ok := args[key]
	if !ok {
		return fallback
	}
	switch typed := value.(type) {
	case int:
		return typed
	case float64:
		return int(typed)
	default:
		return fallback
	}
}

func durationArg(args map[string]any, key string, fallback time.Duration) time.Duration {
	value, ok := args[key]
	if !ok {
		return fallback
	}
	switch typed := value.(type) {
	case int:
		return time.Duration(typed) * time.Second
	case int64:
		return time.Duration(typed) * time.Second
	case float64:
		return time.Duration(typed * float64(time.Second))
	case string:
		parsed, err := time.ParseDuration(typed)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

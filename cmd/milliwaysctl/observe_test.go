package main

import "testing"

func TestNormalizeObserveSecurityStatus(t *testing.T) {
	t.Parallel()

	got := normalizeObserveSecurityStatus(map[string]any{
		"installed":     true,
		"enabled":       true,
		"mode":          "strict",
		"posture":       "warn",
		"warning_count": float64(2),
		"block_count":   float64(0),
	})

	if got["posture"] != "warn" {
		t.Fatalf("posture = %v, want warn", got["posture"])
	}
	if got["warnings"] != 2 {
		t.Fatalf("warnings = %v, want 2", got["warnings"])
	}
	if got["blocks"] != 0 {
		t.Fatalf("blocks = %v, want 0", got["blocks"])
	}
	if got["mode"] != "strict" {
		t.Fatalf("mode = %v, want strict", got["mode"])
	}
	if got["installed"] != true {
		t.Fatalf("installed = %v, want true", got["installed"])
	}
}

func TestNormalizeObserveSecurityStatusInfersBlockPosture(t *testing.T) {
	t.Parallel()

	got := normalizeObserveSecurityStatus(map[string]any{
		"state":    "ok",
		"blocks":   float64(1),
		"warnings": float64(3),
	})

	if got["posture"] != "block" {
		t.Fatalf("posture = %v, want block", got["posture"])
	}
	if got["blocks"] != 1 {
		t.Fatalf("blocks = %v, want 1", got["blocks"])
	}
}

func TestNormalizeObserveSecurityStatusPreservesPolicyShape(t *testing.T) {
	t.Parallel()

	enforcement := map[string]any{
		"codex": map[string]any{"level": "brokered", "controlled_env": true},
		"local": map[string]any{"level": "full"},
	}
	scanners := []any{map[string]any{"name": "gitleaks", "installed": false}}
	got := normalizeObserveSecurityStatus(map[string]any{
		"mode":                      "strict",
		"posture":                   "ok",
		"active_client":             "codex",
		"workspace":                 "/repo",
		"security_workspace":        "/repo/service",
		"startup_scan_completed":    true,
		"startup_scan_required":     true,
		"startup_scan_stale":        true,
		"startup_scan_completed_at": "2026-05-14T10:00:00Z",
		"last_startup_scan_at":      "2026-05-14T10:00:00Z",
		"last_dependency_scan_at":   "2026-05-14T10:05:00Z",
		"client_enforcement":        enforcement,
		"scanners":                  scanners,
	})

	for _, key := range []string{
		"active_client",
		"workspace",
		"security_workspace",
		"startup_scan_completed",
		"startup_scan_required",
		"startup_scan_stale",
		"startup_scan_completed_at",
		"last_startup_scan_at",
		"last_dependency_scan_at",
		"client_enforcement",
		"scanners",
	} {
		if _, ok := got[key]; !ok {
			t.Fatalf("normalized status dropped %q: %#v", key, got)
		}
	}
	if got["posture"] != "warn" {
		t.Fatalf("posture = %v, want warn when startup scan is stale/required", got["posture"])
	}
}

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

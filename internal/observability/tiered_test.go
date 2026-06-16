package observability

import "testing"

func TestTieredEventUsesScopeDigestWithoutPrivatePaths(t *testing.T) {
	event := NewTieredExecutionEvent("session-1", []string{"internal/tiered", "cmd/milliways"})
	event.Tier = "tier-1-specialist"
	event.GPURuntime = "rocm-7.2.4"
	event.GFXTarget = "gfx1200"
	event.OffloadedLayers = 49
	event.PeakMemoryBytes = 9178537984
	if !event.SafeForExport() {
		t.Fatalf("event should be safe: %#v", event)
	}
	if event.ScopeDigest == "" {
		t.Fatal("scope digest missing")
	}
}

func TestTieredEventRejectsPromptSecretOrPrivatePathFields(t *testing.T) {
	event := NewTieredExecutionEvent("session-1", []string{"src"})
	event.RoutingReason = "Bearer secret"
	if event.SafeForExport() {
		t.Fatal("secret-bearing event should not export")
	}
	event.RoutingReason = "/home/operator/private/model.gguf"
	if event.SafeForExport() {
		t.Fatal("private path event should not export")
	}
}

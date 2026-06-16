package tiered

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mwigge/milliways/internal/kitchen/adapter"
)

func TestEndToEndOutcomeDecisions(t *testing.T) {
	tests := []struct {
		status   string
		attempts int
		fallback bool
		want     string
	}{
		{"verified", 0, false, "accept"},
		{"failed", 0, false, "repair"},
		{"failed", 1, true, "fallback"},
		{"failed", 1, false, "supervisor-takeover"},
		{"scope-violation", 0, true, "supervisor-takeover"},
		{"stale-revision", 0, true, "supervisor-takeover"},
	}
	for _, test := range tests {
		if got := DecideOutcome(test.status, test.attempts, 1, test.fallback); got != test.want {
			t.Errorf("DecideOutcome(%q) = %q, want %q", test.status, got, test.want)
		}
	}
}

func TestAgentCapabilityNegotiationHasNoHardCodedPlanner(t *testing.T) {
	// DirectRunOnly and SupervisoryQualified are derived independently
	// (PolicyForAgent vs SupervisorCapabilityReport) but must always be
	// logical opposites: an agent that is qualified to supervise (plan,
	// delegate, review, and resume work) should never be restricted to
	// direct-run-only, and an agent that is not supervisory-qualified must
	// always be direct-run-only. Equal values here mean the two reports have
	// drifted out of sync for this agent.
	for _, agent := range []string{"claude", "codex", "gemini", "copilot", "pool"} {
		caps := adapter.SupervisorCapabilityReport(agent)
		policy := adapter.PolicyForAgent(agent)
		if policy.DirectRunOnly == caps.SupervisoryQualified() {
			t.Errorf("%s: DirectRunOnly=%v and SupervisoryQualified()=%v must be opposites, got equal values",
				agent, policy.DirectRunOnly, caps.SupervisoryQualified())
		}
	}
}

// repoRoot returns the repository root, resolved relative to this test
// file's location so the test does not depend on the working directory the
// test runner uses.
func repoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func TestFourLanguageReadinessEvidenceUsesQualifiedLocalBackend(t *testing.T) {
	path := filepath.Join(repoRoot(t), "evidence", "readiness", "2026-06-15-gemma4-four-language-rocm.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var evidence struct {
		Qualified bool            `json:"qualified"`
		Backend   string          `json:"backend"`
		Languages map[string]bool `json:"languages"`
	}
	if err := json.Unmarshal(data, &evidence); err != nil {
		t.Fatal(err)
	}
	if !evidence.Qualified || evidence.Backend != "rocm" {
		t.Fatalf("evidence = %#v", evidence)
	}
	for _, language := range []string{"go", "rust", "python", "typescript"} {
		if !evidence.Languages[language] {
			t.Errorf("%s readiness missing", language)
		}
	}
}

func TestTierOneRemainsOptInUntilAllThresholdsPass(t *testing.T) {
	gate := RolloutGate{
		Mode: RolloutLive, SelectedRepositories: []string{"/repo"},
		QualityPassed: false, LatencyPassed: true, SafetyPassed: true, RecoveryPassed: true,
	}
	if gate.EnabledFor("/repo") || gate.CanRemoveOptIn() {
		t.Fatal("live Tier 1 enabled before quality threshold passed")
	}
	gate.Mode = RolloutShadow
	if !gate.EnabledFor("/repo") {
		t.Fatal("selected repository should be eligible for shadow metrics")
	}
}

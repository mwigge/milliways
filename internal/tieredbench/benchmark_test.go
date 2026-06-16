package tieredbench

import (
	"context"
	"testing"
	"time"
)

type fakeRunner struct{}

func (fakeRunner) Generate(_ context.Context, _ Candidate, benchmark Case) (Generation, error) {
	source := map[string]string{
		"go":         "package main\nfunc add(a, b int) int { return a + b }",
		"rust":       "fn add(a: i32, b: i32) -> i32 { a + b }",
		"python":     "def add(a: int, b: int) -> int:\n    return a + b",
		"typescript": "function add(a: number, b: number): number { return a + b; }",
	}[benchmark.Language]
	return Generation{Text: source, Latency: 100 * time.Millisecond, MemoryBytes: 1024, VerifierPass: true}, nil
}

func TestDefaultSuiteCoversFourLanguagesAndTaskKinds(t *testing.T) {
	suite := DefaultSuite()
	if len(suite) != 16 {
		t.Fatalf("suite length = %d, want 16", len(suite))
	}
}

func TestShadowBenchmarkNeverAppliesEditsAndQualifiesMultipleLanguages(t *testing.T) {
	candidate := Candidate{Model: "gemma4", Backend: "rocm", HardwareClass: "gfx1200-16g"}
	evidence := Run(context.Background(), fakeRunner{}, candidate, DefaultSuite(), true)
	if evidence.AppliedEdits {
		t.Fatal("shadow benchmark applied edits")
	}
	promotion := Qualify(evidence, Thresholds{
		MinimumQuality: 1, MaximumLatency: time.Second, RequiredPassRate: 1,
	}, "rocm", "gfx1200-16g")
	if !promotion.Promoted || len(promotion.Capabilities) != 4 {
		t.Fatalf("promotion = %#v", promotion)
	}
}

func TestPromotionRequiresMatchingBackendAndHardware(t *testing.T) {
	evidence := Run(context.Background(), fakeRunner{}, Candidate{
		Model: "gemma4", Backend: "rocm", HardwareClass: "gfx1200-16g",
	}, DefaultSuite(), true)
	promotion := Qualify(evidence, Thresholds{
		MinimumQuality: 0, MaximumLatency: time.Second, RequiredPassRate: 0,
	}, "metal", "apple-silicon")
	if promotion.Promoted {
		t.Fatal("candidate promoted on an unmeasured backend")
	}
}

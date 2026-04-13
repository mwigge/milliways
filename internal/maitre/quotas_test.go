package maitre

import (
	"path/filepath"
	"testing"

	"github.com/mwigge/milliways/internal/pantry"
)

func openTestPantry(t *testing.T) *pantry.DB {
	t.Helper()
	pdb, err := pantry.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pdb.Close() })
	return pdb
}

func TestQuotaCheck_AllowedByDefault(t *testing.T) {
	t.Parallel()
	pdb := openTestPantry(t)
	qc := NewQuotaCheck(pdb, nil, GlobalQuotaConfig{})

	result := qc.Check("claude")
	if !result.Allowed {
		t.Errorf("expected allowed, got denied: %s", result.Reason)
	}
}

func TestQuotaCheck_DailyLimit(t *testing.T) {
	t.Parallel()
	pdb := openTestPantry(t)

	// Simulate 5 dispatches today
	for range 5 {
		_ = pdb.Quotas().Increment("claude", 1.0, false)
	}

	qc := NewQuotaCheck(pdb,
		map[string]QuotaConfig{"claude": {DailyDispatches: 5}},
		GlobalQuotaConfig{},
	)

	result := qc.Check("claude")
	if result.Allowed {
		t.Error("expected denied (daily limit reached)")
	}
	if result.Reason == "" {
		t.Error("expected non-empty reason")
	}
}

func TestQuotaCheck_DailyLimitNotReached(t *testing.T) {
	t.Parallel()
	pdb := openTestPantry(t)

	_ = pdb.Quotas().Increment("claude", 1.0, false)

	qc := NewQuotaCheck(pdb,
		map[string]QuotaConfig{"claude": {DailyDispatches: 10}},
		GlobalQuotaConfig{},
	)

	result := qc.Check("claude")
	if !result.Allowed {
		t.Errorf("expected allowed (1/10), got denied: %s", result.Reason)
	}
}

func TestQuotaCheck_NoQuotaConfigured(t *testing.T) {
	t.Parallel()
	pdb := openTestPantry(t)

	// No quota for opencode
	qc := NewQuotaCheck(pdb,
		map[string]QuotaConfig{"claude": {DailyDispatches: 5}},
		GlobalQuotaConfig{},
	)

	result := qc.Check("opencode")
	if !result.Allowed {
		t.Errorf("expected allowed (no quota configured), got denied: %s", result.Reason)
	}
}

func TestQuotaCheck_SystemMemory(t *testing.T) {
	t.Parallel()
	// Can't test the actual memory check deterministically,
	// but verify the function doesn't crash
	pct := systemMemoryPercent()
	if pct < 0 || pct > 100 {
		t.Errorf("memory percent out of range: %d", pct)
	}
}

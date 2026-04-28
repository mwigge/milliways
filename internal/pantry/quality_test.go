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

package pantry

import "testing"

func TestQualityStore_UpsertAndFileRisk(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	qs := db.Quality()

	// Upsert two functions in the same file
	if err := qs.Upsert("/repo", QualityMetrics{
		FilePath: "store.py", FunctionName: "list_runs",
		CyclomaticComplexity: 12, CognitiveComplexity: 8,
		CoveragePct: 85, SmellCount: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := qs.Upsert("/repo", QualityMetrics{
		FilePath: "store.py", FunctionName: "get_run",
		CyclomaticComplexity: 34, CognitiveComplexity: 22,
		CoveragePct: 45, SmellCount: 3,
	}); err != nil {
		t.Fatal(err)
	}

	// FileRisk should return max complexity, min coverage, sum smells
	risk, err := qs.FileRisk("/repo", "store.py")
	if err != nil {
		t.Fatal(err)
	}
	if risk == nil {
		t.Fatal("expected risk data")
	}
	if risk.CyclomaticComplexity != 34 {
		t.Errorf("cyclomatic: got %d, want 34 (max)", risk.CyclomaticComplexity)
	}
	if risk.CoveragePct != 45 {
		t.Errorf("coverage: got %.0f, want 45 (min)", risk.CoveragePct)
	}
	if risk.SmellCount != 4 {
		t.Errorf("smells: got %d, want 4 (sum)", risk.SmellCount)
	}
}

func TestQualityStore_FileRisk_NotFound(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	risk, err := db.Quality().FileRisk("/repo", "nonexistent.go")
	if err != nil {
		t.Fatal(err)
	}
	if risk != nil {
		t.Error("expected nil for nonexistent file")
	}
}

func TestQualityStore_UpsertIdempotent(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	qs := db.Quality()

	m := QualityMetrics{FilePath: "api.go", FunctionName: "handler", CyclomaticComplexity: 5, CoveragePct: 90}
	if err := qs.Upsert("/repo", m); err != nil {
		t.Fatal(err)
	}

	// Upsert again with different values
	m.CyclomaticComplexity = 10
	m.CoveragePct = 75
	if err := qs.Upsert("/repo", m); err != nil {
		t.Fatal(err)
	}

	risk, err := qs.FileRisk("/repo", "api.go")
	if err != nil {
		t.Fatal(err)
	}
	if risk.CyclomaticComplexity != 10 {
		t.Errorf("expected updated complexity 10, got %d", risk.CyclomaticComplexity)
	}
}

func TestQualityStore_ImportCoverage(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	coverage := map[string]float64{
		"store.py":   82.5,
		"routes.py":  91.0,
		"handler.go": 65.3,
	}

	count, err := db.Quality().ImportCoverage("/repo", coverage)
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Errorf("expected 3 files imported, got %d", count)
	}

	risk, err := db.Quality().FileRisk("/repo", "store.py")
	if err != nil {
		t.Fatal(err)
	}
	if risk == nil {
		t.Fatal("expected risk data after import")
	}
	if risk.CoveragePct != 82.5 {
		t.Errorf("expected 82.5%% coverage, got %.1f", risk.CoveragePct)
	}
}

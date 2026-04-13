package pantry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDepStore_UpsertAndHasCVE(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	ds := db.Deps()

	if err := ds.Upsert("/repo", DepInfo{Package: "vulnerable-pkg", Version: "1.0.0", CVEIDs: "CVE-2024-001,CVE-2024-002", LockFile: "go.mod"}); err != nil {
		t.Fatal(err)
	}
	if err := ds.Upsert("/repo", DepInfo{Package: "safe-pkg", Version: "2.0.0", LockFile: "go.mod"}); err != nil {
		t.Fatal(err)
	}

	cves, err := ds.HasCVE("/repo", "vulnerable-pkg")
	if err != nil {
		t.Fatal(err)
	}
	if cves != "CVE-2024-001,CVE-2024-002" {
		t.Errorf("expected CVEs, got %q", cves)
	}

	cves, err = ds.HasCVE("/repo", "safe-pkg")
	if err != nil {
		t.Fatal(err)
	}
	if cves != "" {
		t.Errorf("expected no CVEs, got %q", cves)
	}
}

func TestDepStore_SyncGoMod(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	dir := t.TempDir()

	gomod := `module github.com/test/project

go 1.22

require (
	github.com/spf13/cobra v1.10.2
	github.com/mattn/go-sqlite3 v1.14.42
	gopkg.in/yaml.v3 v3.0.1
)
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}

	count, err := db.Deps().SyncGoMod(dir, filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Errorf("expected 3 deps, got %d", count)
	}
}

func TestDepStore_SyncPackageJSON(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	dir := t.TempDir()

	pkg := `{
  "name": "test",
  "dependencies": {
    "express": "^4.18.0",
    "lodash": "^4.17.21"
  },
  "devDependencies": {
    "vitest": "^1.0.0"
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkg), 0o644); err != nil {
		t.Fatal(err)
	}

	count, err := db.Deps().SyncPackageJSON(dir, filepath.Join(dir, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Errorf("expected 3 deps, got %d", count)
	}
}

func TestDepStore_SyncAuto(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	dir := t.TempDir()
	gomod := `module github.com/test/auto

go 1.22

require (
	github.com/spf13/cobra v1.10.2
)
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}

	count, err := db.Deps().SyncAuto(dir)
	if err != nil {
		t.Fatalf("SyncAuto: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 dep from go.mod, got %d", count)
	}
}

func TestDepStore_HasCVE_NotFound(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	cves, err := db.Deps().HasCVE("/repo", "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if cves != "" {
		t.Errorf("expected empty, got %q", cves)
	}
}

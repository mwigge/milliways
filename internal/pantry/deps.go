package pantry

import (
	"bufio"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DepStore provides access to the mw_deps table.
type DepStore struct {
	db *sql.DB
}

// DepInfo holds dependency information for a package.
type DepInfo struct {
	Package       string
	Version       string
	LatestVersion string
	CVEIDs        string
	LockFile      string
}

// Upsert inserts or updates a dependency record.
func (s *DepStore) Upsert(repo string, d DepInfo) error {
	_, err := s.db.Exec(`
		INSERT INTO mw_deps (repo, package, version, latest_version, cve_ids, lock_file, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(repo, package, lock_file) DO UPDATE SET
			version = excluded.version,
			latest_version = excluded.latest_version,
			cve_ids = excluded.cve_ids,
			updated_at = datetime('now')
	`, repo, d.Package, d.Version, d.LatestVersion, d.CVEIDs, d.LockFile)
	if err != nil {
		return fmt.Errorf("upserting dep: %w", err)
	}
	return nil
}

// HasCVE checks if a package has known CVEs.
func (s *DepStore) HasCVE(repo, pkg string) (string, error) {
	var cves string
	err := s.db.QueryRow(`
		SELECT COALESCE(cve_ids, '') FROM mw_deps
		WHERE repo = ? AND package = ? AND cve_ids != ''
	`, repo, pkg).Scan(&cves)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("querying CVEs: %w", err)
	}
	return cves, nil
}

// SyncGoMod parses a go.mod file and upserts dependencies.
func (s *DepStore) SyncGoMod(repo, goModPath string) (int, error) {
	f, err := os.Open(goModPath)
	if err != nil {
		return 0, fmt.Errorf("opening go.mod: %w", err)
	}
	defer func() { _ = f.Close() }()

	count := 0
	inRequire := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "require (" {
			inRequire = true
			continue
		}
		if line == ")" {
			inRequire = false
			continue
		}
		if !inRequire {
			continue
		}

		// Parse "module/path v1.2.3"
		parts := strings.Fields(line)
		if len(parts) < 2 || strings.HasPrefix(parts[0], "//") {
			continue
		}

		if err := s.Upsert(repo, DepInfo{
			Package:  parts[0],
			Version:  parts[1],
			LockFile: "go.mod",
		}); err != nil {
			return count, err
		}
		count++
	}
	return count, scanner.Err()
}

// SyncPackageJSON parses a package.json file for dependencies.
func (s *DepStore) SyncPackageJSON(repo, pkgPath string) (int, error) {
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return 0, fmt.Errorf("reading package.json: %w", err)
	}

	// Simple line-based parser — avoids JSON dependency
	count := 0
	inDeps := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, `"dependencies"`) || strings.Contains(trimmed, `"devDependencies"`) {
			inDeps = true
			continue
		}
		if inDeps && trimmed == "}" {
			inDeps = false
			continue
		}
		if !inDeps {
			continue
		}

		// Parse `"package": "^1.2.3"`
		trimmed = strings.TrimSuffix(trimmed, ",")
		parts := strings.SplitN(trimmed, ":", 2)
		if len(parts) != 2 {
			continue
		}
		pkg := strings.Trim(strings.TrimSpace(parts[0]), `"`)
		ver := strings.Trim(strings.TrimSpace(parts[1]), `"`)
		if pkg == "" || ver == "" {
			continue
		}

		if err := s.Upsert(repo, DepInfo{
			Package:  pkg,
			Version:  ver,
			LockFile: "package.json",
		}); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

// SyncAuto detects lock files in a repo and syncs all found.
func (s *DepStore) SyncAuto(repoPath string) (int, error) {
	total := 0

	goMod := filepath.Join(repoPath, "go.mod")
	if _, err := os.Stat(goMod); err == nil {
		n, err := s.SyncGoMod(repoPath, goMod)
		if err != nil {
			return total, err
		}
		total += n
	}

	pkgJSON := filepath.Join(repoPath, "package.json")
	if _, err := os.Stat(pkgJSON); err == nil {
		n, err := s.SyncPackageJSON(repoPath, pkgJSON)
		if err != nil {
			return total, err
		}
		total += n
	}

	return total, nil
}

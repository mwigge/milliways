## ADDED Requirements

### Requirement: Repo detection from working directory

The system SHALL detect the project repository by walking up from the current working directory looking for a `.git/` directory.

#### Scenario: Git repo found in cwd
- **WHEN** milliways starts in `/home/user/projects/acme-saas/src/`
- **AND** `/home/user/projects/acme-saas/.git/` exists
- **THEN** repo_root SHALL be set to `/home/user/projects/acme-saas`

#### Scenario: Git repo found in parent
- **WHEN** milliways starts in `/home/user/projects/acme-saas/src/components/`
- **AND** `.git/` exists at `/home/user/projects/acme-saas/.git/`
- **THEN** repo_root SHALL be set to `/home/user/projects/acme-saas`

#### Scenario: No git repo found
- **WHEN** milliways starts in `/tmp/scratch/`
- **AND** no `.git/` directory exists in `/tmp/scratch/` or any parent up to `/`
- **THEN** milliways SHALL exit with error "No project repository found. Run from within a git repo or specify --project-root"

### Requirement: Manual repo root override

The system SHALL accept a `--project-root` flag to explicitly specify the repository root.

#### Scenario: Override with valid path
- **WHEN** milliways starts with `--project-root /home/user/projects/acme-saas`
- **AND** `/home/user/projects/acme-saas/.git/` exists
- **THEN** repo_root SHALL be set to `/home/user/projects/acme-saas`
- **AND** cwd-based detection SHALL be skipped

#### Scenario: Override with invalid path
- **WHEN** milliways starts with `--project-root /nonexistent`
- **THEN** milliways SHALL exit with error "Project root does not exist: /nonexistent"

#### Scenario: Override with non-repo path
- **WHEN** milliways starts with `--project-root /tmp`
- **AND** `/tmp/.git/` does not exist
- **THEN** milliways SHALL exit with error "No git repository at /tmp"

### Requirement: CodeGraph auto-initialization

The system SHALL automatically initialize CodeGraph if `.codegraph/` does not exist in the repo root.

#### Scenario: CodeGraph exists
- **WHEN** repo_root is `/home/user/projects/acme-saas`
- **AND** `/home/user/projects/acme-saas/.codegraph/` exists
- **THEN** CodeGraph index SHALL be loaded from that path

#### Scenario: CodeGraph missing, auto-init
- **WHEN** repo_root is `/home/user/projects/acme-saas`
- **AND** `/home/user/projects/acme-saas/.codegraph/` does not exist
- **THEN** CodeGraph SHALL be initialized in background
- **AND** TUI status SHALL show "CodeGraph: indexing..."

### Requirement: Project palace detection

The system SHALL detect an optional project palace at `.mempalace/` in the repo root.

#### Scenario: Palace exists
- **WHEN** repo_root is `/home/user/projects/acme-saas`
- **AND** `/home/user/projects/acme-saas/.mempalace/` exists
- **THEN** project_palace SHALL be set to that path
- **AND** TUI status SHALL show palace drawer count

#### Scenario: Palace missing, graceful degradation
- **WHEN** repo_root is `/home/user/projects/acme-saas`
- **AND** `/home/user/projects/acme-saas/.mempalace/` does not exist
- **THEN** project_palace SHALL be nil
- **AND** TUI status SHALL show "palace: (none)"
- **AND** project memory features SHALL be disabled without error

### Requirement: Project context structure

The system SHALL populate a ProjectContext struct with resolved project information.

#### Scenario: Full context with palace
- **WHEN** project resolution completes successfully
- **AND** both CodeGraph and palace exist
- **THEN** ProjectContext SHALL contain:
  - repo_root: absolute path to repo
  - repo_name: directory name of repo_root
  - codegraph_path: path to .codegraph/
  - palace_path: path to .mempalace/ (non-nil)
  - access_rules: from registry or defaults

#### Scenario: Context without palace
- **WHEN** project resolution completes successfully
- **AND** CodeGraph exists but palace does not
- **THEN** ProjectContext SHALL contain:
  - repo_root, repo_name, codegraph_path: populated
  - palace_path: nil
  - access_rules: from registry or defaults
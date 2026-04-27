## ADDED Requirements

### Requirement: Wezterm fork lives at `crates/milliways-term/`

Milliways SHALL maintain a fork of wezterm imported as a `git subtree` at `crates/milliways-term/`. The fork SHALL be a Cargo workspace member of the milliways monorepo so a single `cargo build` can compile both upstream wezterm crates and milliways-specific additions.

#### Scenario: Subtree import on first setup

- **WHEN** a contributor runs `make setup` on a fresh checkout
- **THEN** `crates/milliways-term/` SHALL contain wezterm sources at the pinned upstream commit hash recorded in `PATCHES.md`
- **AND** the root `Cargo.toml` SHALL exclude `crates/milliways-term` from its own workspace (wezterm has its own nested workspace) and `cargo build --release --manifest-path crates/milliways-term/Cargo.toml -p wezterm-gui` SHALL produce a working terminal binary

#### Scenario: Binary renamed to `milliways-term`

- **WHEN** the wezterm GUI binary is built
- **THEN** the resulting executable SHALL be named `milliways-term` (not `wezterm-gui`)
- **AND** the patch that performs this rename SHALL be recorded in `crates/milliways-term/PATCHES.md` with a one-line rationale

### Requirement: All milliways-specific code lives in `milliways/` subtree

Additions to the fork SHALL be confined to `crates/milliways-term/milliways/` (a sibling crate to upstream `wezterm/`, `wezterm-gui/`, `mux/`, etc.). Patches to upstream files SHALL be minimised; the project SHALL aim for under 500 lines of patched upstream code at any time.

#### Scenario: New feature adds no upstream patches

- **WHEN** a feature can be implemented without modifying any file under `crates/milliways-term/wezterm-*/` or `crates/milliways-term/mux/`
- **THEN** the implementation SHALL live entirely under `crates/milliways-term/milliways/`
- **AND** the feature SHALL NOT add lines to `PATCHES.md`

#### Scenario: New feature requires an upstream patch

- **WHEN** an upstream patch is unavoidable (e.g., a hook call from `wezterm-gui::main`)
- **THEN** the patched lines SHALL total no more than 10 per file unless explicitly approved in a design doc
- **AND** each patched file SHALL be listed in `PATCHES.md` with file path, patched lines count, and one-line rationale

### Requirement: Upstream sync via merge, not rebase

The fork SHALL track upstream wezterm via `git merge`. Rebasing onto upstream is forbidden because it would invalidate review history of the milliways subtree.

#### Scenario: Sync at milliways release or monthly minimum

- **WHEN** a maintainer performs an upstream sync — triggered either by a new milliways release being prepared, or because 30+ days have elapsed since the last sync
- **THEN** the sync SHALL use `git subtree merge --prefix crates/milliways-term --squash <wezterm-repo> <new-commit>`
- **AND** the new pinned commit hash SHALL be recorded at the top of `PATCHES.md`
- **AND** any merge conflict in patched files SHALL be resolved manually with the resolution recorded in the merge commit body

#### Scenario: CI catches stale fork

- **WHEN** the pinned commit is more than 60 days behind `wezterm/main`
- **THEN** a CI job SHALL warn (not fail) on every PR until the sync is performed

### Requirement: Apache-2.0 attribution preserved

Wezterm is licensed Apache-2.0. The fork SHALL comply with all four conditions of §4 of the licence:

- §4(a) Recipients receive a copy: `LICENSE` and `NOTICE` files SHALL be preserved unchanged at `crates/milliways-term/`.
- §4(b) Modified files SHALL carry prominent notices stating that they were changed: every upstream-patched file SHALL retain its original wezterm copyright header and SHALL gain a one-line `// Modified by milliways contributors, <year>` notice immediately below.
- §4(c) Per-file copyright/notice retention: original copyright, patent, trademark, and attribution notices SHALL NOT be removed from any source file.
- §4(d) The shipped binary SHALL include the `NOTICE` content. `milliways-term --notice` SHALL print the bundled `LICENSE` followed by the bundled `NOTICE` followed by `MILLIWAYS_NOTICE.md`. The repo root SHALL include `MILLIWAYS_NOTICE.md` documenting milliways' additions and crediting wezterm.

#### Scenario: License files intact in shipped binary

- **WHEN** a build is shipped
- **THEN** running `milliways-term --notice` SHALL print all three documents in order
- **AND** wezterm's copyright notices SHALL NOT be removed from any source file
- **AND** every patched upstream file SHALL contain the `// Modified by milliways contributors` annotation

### Requirement: Rust toolchain pinned

The fork SHALL pin the Rust toolchain version in `rust-toolchain.toml` to whatever upstream wezterm pins, plus a milliways-specific override only if absolutely required.

#### Scenario: Toolchain mismatch

- **WHEN** a contributor's local toolchain differs from `rust-toolchain.toml`
- **THEN** `rustup` SHALL automatically install the pinned version
- **AND** CI SHALL use the same pinned version

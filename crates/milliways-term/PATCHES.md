# PATCHES.md — milliways-term

This file is the inventory of every modification milliways makes to the
upstream wezterm subtree. Per the design (`openspec/changes/milliways-emulator-fork/specs/wezterm-fork/spec.md`),
the patch surface stays small (target: <500 lines across upstream files), all
new code lives in `crates/milliways-term/milliways/`, and every patch lands
here with a one-line rationale.

## Pinned upstream

| Field | Value |
|-------|-------|
| Upstream repo | https://github.com/wez/wezterm.git |
| Pinned commit | `577474d89ee61aef4a48145cdec82a638d874751` |
| Pinned date   | 2026-04-27 (fork date) |
| Note          | Wezterm has not tagged a release since 2024-02-03; their de-facto release model is rolling main. Per Decision 1 in `design.md`, we pin a commit hash and treat it as a tag-equivalent. |
| Sync cadence  | At each milliways release, OR every 30 days (whichever first). |

## Vendored submodules

Wezterm tracks four C-source dependencies as git submodules. `git subtree`
does not recurse into submodules, so we vendor each at the exact commit
wezterm pins, also via `git subtree --squash`. This keeps the fork self-
contained — no `git submodule update --init --recursive` step for any
contributor — at the cost of ~5,000 vendored files in our tree.
Reproducing the import requires only `git clone milliways && make term`.

| Submodule | Path inside subtree | Pinned commit | Files |
|-----------|---------------------|---------------|-------|
| freetype2 | `deps/freetype/freetype2` | `42608f77f20749dd6ddc9e0536788eaad70ea4b5` | 734  |
| libpng    | `deps/freetype/libpng`    | `f5e92d76973a7a53f517579bc95d61483bf108c0` | 575  |
| zlib      | `deps/freetype/zlib`      | `51b7f2abdade71cd9bb0e7a373ef2610ec6f9daf` | 257  |
| harfbuzz  | `deps/harfbuzz/harfbuzz`  | `33a3f8de60dcad7535f14f07d6710144548853ac` | 3426 |

Sync procedure for submodules: when a wezterm sync bumps any of the four
upstream submodule pointers, run
`git subtree merge --prefix=<path> --squash <url> <new-commit>` for the
affected submodule and update the row above.

## Patched upstream files

| File | Patched lines | Task | Rationale |
|------|---------------|------|-----------|
| `crates/milliways-term/wezterm-gui/Cargo.toml` | +10 | TASK-2.2 + TASK-2.4 | (2.2) Added `[[bin]] name = "milliways-term"` block so the produced executable is named `milliways-term`. Crate name unchanged. (2.4) Added `milliways = { path = "../milliways" }` dependency. |
| `crates/milliways-term/Cargo.toml` | +5 | TASK-2.3 | Added `"milliways"` to workspace `members` and a comment header. Lets the milliways/ subtree compile alongside wezterm crates without a separate workspace. |
| `crates/milliways-term/wezterm-gui/src/main.rs` | +3 | TASK-2.4 | Inserted `milliways::init();` after `Mux::set_mux(&mux)` so milliways extensions (AgentDomain, Lua API, status helpers) register once the mux exists. |
| `crates/milliways-term/wezterm-gui/src/main.rs` | +10 | apache-notice | Detect `--notice` as the first CLI argument inside `fn main()` — call `milliways::print_notice()` and exit 0 before any wezterm initialisation. Required by Apache-2.0 §4(d) attribution (`wezterm-fork/spec.md`). |

**Total patched lines on upstream files: 28.** Budget: <500 (per `wezterm-fork/spec.md` requirement "All milliways-specific code lives in `milliways/` subtree"). Headroom: 472.

## Sync history

| Date       | From commit | To commit | Conflicts | Resolution notes |
|------------|-------------|-----------|-----------|------------------|
| 2026-04-27 | _initial_   | `577474d8` | _none — initial import_ | Subtree-added with `--squash`. |

## Sync procedure

```bash
# 1. Identify the new pinned commit (latest wezterm/main, or whatever is
#    deemed release-quality at sync time):
NEW=$(git ls-remote https://github.com/wez/wezterm.git refs/heads/main | awk '{print $1}')

# 2. Apply the merge:
git checkout -b chore/wezterm-sync-$(date +%Y%m%d)
git subtree merge --prefix=crates/milliways-term --squash https://github.com/wez/wezterm.git "$NEW"

# 3. If conflicts: resolve manually, prefer keeping milliways patches over
#    upstream changes when patched lines collide.

# 4. Update the "Pinned upstream" section above with the new commit hash
#    and date. Update the sync history table.

# 5. Run the full smoke suite:
make repl
cargo build --manifest-path crates/milliways-term/Cargo.toml -p wezterm-gui
# (plus any milliways-side cargo tests once they exist)

# 6. Open a PR titled "chore: sync wezterm to <short-hash>".
```

## Tracked upstream `Domain` trait surface

The `Domain` trait in `crates/milliways-term/mux/src/domain.rs` is the load-
bearing extension point for `AgentDomain`. Per Decision 11 in `design.md`, any
signature change between the pinned commit and a candidate sync target SHALL
fail CI (not warn).

This table is populated when TASK-3.2 lands; it lists every method milliways
calls and the version at which it was last reviewed.

| Method | Reviewed at commit | Notes |
|--------|--------------------|-------|
| _populated by Phase 3_ | | |

#!/bin/sh
# scripts/smoke.sh - run the milliways smoke rig.
#
# Scenarios:
#   PC-21.1  claude exhausts -> codex continues
#
# Environment:
#   MILLIWAYS_BIN    path to the milliways binary (default: $TMPDIR/milliways
#                    or /tmp/milliways if TMPDIR is unset).
#   SMOKE_KEEP       if non-empty, do not remove the per-run temp dir on
#                    exit. Useful for debugging.
#
# The script creates a per-run temp dir for all runtime state (ledger,
# database, rendered config, HOME/XDG_CONFIG redirects) and removes it on
# exit. The user's real ~/.config/milliways is never touched.

set -u

# --- paths -------------------------------------------------------------

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH= cd -- "$script_dir/.." && pwd)
smoke_root="$repo_root/testdata/smoke"
tmpl="$smoke_root/config/carte.yaml.tmpl"

if [ -z "${TMPDIR:-}" ]; then
    TMPDIR=/tmp
fi
# Strip any trailing slash so path joins stay tidy.
tmp_base=${TMPDIR%/}

bin_default="$tmp_base/milliways"
milliways_bin=${MILLIWAYS_BIN:-$bin_default}

# --- preflight ---------------------------------------------------------

fail() {
    printf '[smoke] FAIL: %s\n' "$1" >&2
    exit 1
}

[ -f "$tmpl" ] || fail "missing config template: $tmpl"
[ -x "$smoke_root/bin/fake-claude-exhausted" ] || fail "missing or non-executable: $smoke_root/bin/fake-claude-exhausted"
[ -x "$smoke_root/bin/fake-codex-ok" ] || fail "missing or non-executable: $smoke_root/bin/fake-codex-ok"

if [ ! -x "$milliways_bin" ]; then
    fail "milliways binary not found at $milliways_bin (set MILLIWAYS_BIN or run 'make smoke')"
fi

# --- per-run temp dir --------------------------------------------------

run_dir=$(mktemp -d "$tmp_base/mw-smoke-XXXXXX") || fail "mktemp failed"

cleanup() {
    if [ -n "${SMOKE_KEEP:-}" ]; then
        printf '[smoke] SMOKE_KEEP set; preserving %s\n' "$run_dir" >&2
        return
    fi
    rm -rf "$run_dir"
}
trap cleanup EXIT HUP INT TERM

# Redirect HOME and XDG_CONFIG_HOME so any writes under ~/.config stay
# inside the run dir. This protects the developer's real milliways config.
export HOME="$run_dir/home"
export XDG_CONFIG_HOME="$run_dir/xdg"
mkdir -p "$HOME" "$XDG_CONFIG_HOME"

# Render the carte.yaml template into the run dir. Use literal pipe
# replacement so path separators never clash with sed.
rendered_config="$run_dir/carte.yaml"
sed \
    -e "s|{{SMOKE_ROOT}}|$smoke_root|g" \
    -e "s|{{RUN_DIR}}|$run_dir|g" \
    "$tmpl" > "$rendered_config" || fail "rendering config template"

# --- scenario PC-21.1 --------------------------------------------------

run_scenario_pc21_1() {
    scenario="PC-21.1 claude-exhausts-codex-continues"
    log="$run_dir/pc21_1.log"

    printf '[smoke] running scenario: %s\n' "$scenario"

    # Capture stdout+stderr together; exit status in $rc. We do not use
    # `set -e` in this script, so a non-zero rc will not abort — we
    # assert on it explicitly below.
    "$milliways_bin" \
        -c "$rendered_config" \
        --verbose \
        --timeout 15s \
        "explain foo" \
        > "$log" 2>&1
    rc=$?

    failures=0

    if [ "$rc" -ne 0 ]; then
        printf '[smoke] FAIL: %s expected exit 0, got %d\n' "$scenario" "$rc" >&2
        failures=$((failures + 1))
    fi

    for needle in \
        "claude exhausted, continuing with the next provider" \
        "[routed] codex"
    do
        if ! grep -Fq -- "$needle" "$log"; then
            printf '[smoke] FAIL: %s missing expected output: %s\n' "$scenario" "$needle" >&2
            failures=$((failures + 1))
        fi
    done

    if [ "$failures" -gt 0 ]; then
        printf '[smoke] --- captured output (%s) ---\n' "$log" >&2
        cat "$log" >&2
        printf '[smoke] --- end captured output ---\n' >&2
        return 1
    fi

    printf '[smoke] pass: %s\n' "$scenario"
    return 0
}

# --- run all -----------------------------------------------------------

overall=0
run_scenario_pc21_1 || overall=1

if [ "$overall" -eq 0 ]; then
    printf '[smoke] all scenarios passed\n'
else
    printf '[smoke] one or more scenarios failed\n' >&2
fi

exit "$overall"

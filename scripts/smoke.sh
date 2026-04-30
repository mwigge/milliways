#!/bin/sh
# scripts/smoke.sh - run the milliways smoke rig.
#
# Scenarios:
#   PC-21.1  claude exhausts -> codex continues
#   KP-19A   user switch -> gpt continues
#   TAM-10.6 milliways exits with error when not in a git repo
#   TAM-10.2 milliways handles missing palace gracefully
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
[ -x "$smoke_root/bin/fake-gpt-ok" ] || fail "missing or non-executable: $smoke_root/bin/fake-gpt-ok"
[ -x "$smoke_root/bin/fake-claude-web-search" ] || fail "missing or non-executable: $smoke_root/bin/fake-claude-web-search"
[ -x "$smoke_root/bin/fake-claude-streaming" ] || fail "missing or non-executable: $smoke_root/bin/fake-claude-streaming"
[ -x "$smoke_root/bin/fake-kitchen-question" ] || fail "missing or non-executable: $smoke_root/bin/fake-kitchen-question"
[ -f "$smoke_root/bin/mock-http-server-main.go" ] || fail "missing: $smoke_root/bin/mock-http-server-main.go"

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
	chmod -R u+w "$run_dir" 2>/dev/null || true
	rm -rf "$run_dir"
}
trap cleanup EXIT HUP INT TERM

# Redirect HOME and XDG_CONFIG_HOME so any writes under ~/.config stay
# inside the run dir. This protects the developer's real milliways config.
export HOME="$run_dir/home"
export XDG_CONFIG_HOME="$run_dir/xdg"
mkdir -p "$HOME" "$XDG_CONFIG_HOME"

scenario_project_root="$run_dir/project"
mkdir -p "$scenario_project_root/.git" "$scenario_project_root/.codegraph"

# Render the carte.yaml template into the run dir. Use literal pipe
# replacement so path separators never clash with sed.
rendered_config="$run_dir/carte.yaml"
sed \
	-e "s|{{SMOKE_ROOT}}|$smoke_root|g" \
	-e "s|{{RUN_DIR}}|$run_dir|g" \
	"$tmpl" >"$rendered_config" || fail "rendering config template"

setup_legacy_project_bridge() {
	mkdir -p "$run_dir/palace" || fail "creating temp palace dir"
	export MEMPALACE_PALACE_PATH="$run_dir/palace"
	export MILLIWAYS_MEMPALACE_MCP_CMD="python3.14 -m mempalace.mcp_server"
}

# --- scenario PC-21.1 --------------------------------------------------

run_scenario_pc21_1() {
	scenario="PC-21.1 claude-exhausts-codex-continues"
	log="$run_dir/pc21_1.log"

	printf '[smoke] running scenario: %s\n' "$scenario"
	setup_legacy_project_bridge

	# Capture stdout+stderr together; exit status in $rc. We do not use
	# `set -e` in this script, so a non-zero rc will not abort — we
	# assert on it explicitly below.
	"$milliways_bin" \
		-c "$rendered_config" \
		--project-root "$scenario_project_root" \
		--use-legacy-conversation \
		--verbose \
		--timeout 15s \
		"explain foo" \
		>"$log" 2>&1
	rc=$?

	failures=0

	if [ "$rc" -ne 0 ]; then
		printf '[smoke] FAIL: %s expected exit 0, got %d\n' "$scenario" "$rc" >&2
		failures=$((failures + 1))
	fi

	for needle in \
		"claude exhausted, continuing with the next provider" \
		"[routed] codex"; do
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

# --- scenario KP-19A ---------------------------------------------------

write_paused_session() {
	session_dir="$HOME/.config/milliways/sessions"
	mkdir -p "$session_dir" || fail "creating session dir"
	cat >"$session_dir/paused.json" <<'SESSION'
{
  "name": "paused",
  "created_at": "2026-04-18T00:00:00Z",
  "updated_at": "2026-04-18T00:00:00Z",
  "blocks": [
    {
      "id": "b1",
      "conversation_id": "conv-1",
      "prompt": "finish the task",
      "kitchen": "claude",
      "provider_chain": ["claude"],
      "state": "waiting for you",
      "lines": [],
      "collapsed": false,
      "exit_code": 0,
      "duration_s": 0,
      "started_at": "2026-04-18T00:00:00Z",
      "conversation": {
        "id": "conv-1",
        "block_id": "b1",
        "prompt": "finish the task",
        "status": "active",
        "transcript": [
          {
            "role": "user",
            "provider": "user",
            "text": "finish the task",
            "at": "2026-04-18T00:00:00Z"
          },
          {
            "role": "assistant",
            "provider": "claude",
            "text": "working on it",
            "at": "2026-04-18T00:00:01Z"
          }
        ],
        "memory": {
          "working_summary": "Keep existing context"
        },
        "context": {},
        "segments": [
          {
            "id": "conv-1-seg-1",
            "provider": "claude",
            "status": "active",
            "started_at": "2026-04-18T00:00:00Z"
          }
        ],
        "active_segment_id": "conv-1-seg-1",
        "created_at": "2026-04-18T00:00:00Z",
        "updated_at": "2026-04-18T00:00:01Z"
      }
    }
  ]
}
SESSION
}

run_scenario_user_switch() {
	scenario="KP-19A user-switch-gpt-continues"
	log="$run_dir/kp19a.log"

	write_paused_session

	printf '[smoke] running scenario: %s\n' "$scenario"
	setup_legacy_project_bridge

	"$milliways_bin" \
		-c "$rendered_config" \
		--project-root "$scenario_project_root" \
		--session paused \
		--switch-to gpt \
		--verbose \
		--timeout 15s \
		"/switch gpt" \
		>"$log" 2>&1
	rc=$?

	failures=0

	if [ "$rc" -ne 0 ]; then
		printf '[smoke] FAIL: %s expected exit 0, got %d\n' "$scenario" "$rc" >&2
		failures=$((failures + 1))
	fi

	for needle in \
		"[switch] session=paused claude -> gpt" \
		"gpt response after switch"; do
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

# --- scenario TAM-10.6: error when starting outside any git repo ---------

run_scenario_tam_10_6() {
	scenario="TAM-10.6 no-git-repo-error"
	log="$run_dir/tam_10_6.log"

	printf '[smoke] running scenario: %s\n' "$scenario"

	# Create a temp dir that is NOT a git repo
	non_git_dir=$(mktemp -d "$tmp_base/mw-no-git-XXXXXX") || fail "mktemp failed"
	# Keep it but do NOT create .git/ inside it

	# milliways should exit with error since it's not inside a git repo
	"$milliways_bin" \
		-c "$rendered_config" \
		--timeout 5s \
		--project-root "$non_git_dir" \
		"hello" \
		>"$log" 2>&1
	rc=$?

	# Clean up the non-git dir
	rm -rf "$non_git_dir"

	failures=0

	if [ "$rc" -eq 0 ]; then
		printf '[smoke] FAIL: %s expected non-zero exit, got 0\n' "$scenario" >&2
		failures=$((failures + 1))
	fi

	# Verify error message is helpful
	for needle in \
		"no git repository"; do
		if ! grep -qi -- "$needle" "$log"; then
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

# --- scenario TAM-10.2: graceful degradation without palace -------------

run_scenario_tam_10_2() {
	scenario="TAM-10.2 no-palace-graceful-degradation"
	log="$run_dir/tam_10_2.log"

	printf '[smoke] running scenario: %s\n' "$scenario"

	# Create a temp git repo WITHOUT .mempalace/ (but WITH .codegraph/ placeholder)
	git_dir=$(mktemp -d "$tmp_base/mw-no-palace-XXXXXX") || fail "mktemp failed"
	mkdir "$git_dir/.git" || fail "creating .git"
	# milliways needs .codegraph to be present (even if empty) for the graceful path
	# Since the fake providers don't do real CodeGraph calls, this should work

	"$milliways_bin" \
		-c "$rendered_config" \
		--project-root "$git_dir" \
		--timeout 15s \
		"hello" \
		>"$log" 2>&1
	rc=$?

	# Clean up
	rm -rf "$git_dir"

	failures=0

	# milliways should NOT crash; it should handle gracefully
	# We just check it doesn't segfault or panic
	if [ "$rc" -gt 128 ]; then
		printf '[smoke] FAIL: %s crashed (signal %d)\n' "$scenario" "$rc" >&2
		failures=$((failures + 1))
	fi

	# Should mention palace or gracefully skip it
	# The output should NOT contain panic or segfault
	if grep -qi "panic\|segmentation fault\|trace/BUG" "$log"; then
		printf '[smoke] FAIL: %s crashed unexpectedly\n' "$scenario" >&2
		failures=$((failures + 1))
	fi

	if [ "$failures" -gt 0 ]; then
		printf '[smoke] --- captured output (%s) ---\n' "$log" >&2
		cat "$log" >&2
		printf '[smoke] --- end captured output ---\n' >&2
		return 1
	fi

	printf '[smoke] pass: %s\n' "$scenario"
	return 0
}

run_scenario_kp22_2() {
	scenario="KP-22.2 auto-switch-detection"
	log="$run_dir/kp22_2.log"

	printf '[smoke] running scenario: %s\n' "$scenario"
	setup_legacy_project_bridge

	"$milliways_bin" \
		-c "$rendered_config" \
		--project-root "$scenario_project_root" \
		--use-legacy-conversation \
		--kitchen claude-web-search \
		--timeout 15s \
		"search the web for foo" \
		>"$log" 2>&1
	rc=$?

	failures=0
	if [ "$rc" -ne 0 ]; then
		printf '[smoke] FAIL: %s expected exit 0, got %d\n' "$scenario" "$rc" >&2
		failures=$((failures + 1))
	fi

	for needle in \
		"search the web" \
		"Let me search the web for that information"; do
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

run_scenario_tp9_4() {
	scenario="TP-9.4 routing-flow"
	log="$run_dir/tp9_4.log"

	printf '[smoke] running scenario: %s\n' "$scenario"
	setup_legacy_project_bridge

	"$milliways_bin" \
		-c "$rendered_config" \
		--project-root "$scenario_project_root" \
		--use-legacy-conversation \
		--verbose \
		--timeout 15s \
		"route this request" \
		>"$log" 2>&1
	rc=$?

	failures=0
	if [ "$rc" -ne 0 ]; then
		printf '[smoke] FAIL: %s expected exit 0, got %d\n' "$scenario" "$rc" >&2
		failures=$((failures + 1))
	fi

	for needle in \
		"[routed] claude-streaming" \
		"Hello from claude"; do
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

run_scenario_tp9_5() {
	scenario="TP-9.5 dialogue-headless"
	log="$run_dir/tp9_5.log"

	printf '[smoke] running scenario: %s\n' "$scenario"
	setup_legacy_project_bridge

	"$milliways_bin" \
		-c "$rendered_config" \
		--project-root "$scenario_project_root" \
		--use-legacy-conversation \
		--kitchen question-kitchen \
		--timeout 15s \
		"ask a question" \
		>"$log" 2>&1
	rc=$?

	failures=0
	if [ "$rc" -ne 0 ]; then
		printf '[smoke] FAIL: %s expected exit 0, got %d\n' "$scenario" "$rc" >&2
		failures=$((failures + 1))
	fi

	if ! grep -Fq -- "Got your answer: answered" "$log"; then
		printf '[smoke] FAIL: %s missing expected output: Got your answer: answered\n' "$scenario" >&2
		failures=$((failures + 1))
	fi

	if [ "$failures" -gt 0 ]; then
		printf '[smoke] --- captured output (%s) ---\n' "$log" >&2
		cat "$log" >&2
		printf '[smoke] --- end captured output ---\n' >&2
		return 1
	fi

	printf '[smoke] pass: %s\n' "$scenario"
	return 0
}

run_scenario_hk5_2() {
	scenario="HK-5.2 http-kitchen-mock"
	log="$run_dir/hk5_2.log"
	http_config="$run_dir/http-carte.yaml"
	mock_port=19999
	mock_server_bin="$run_dir/mock-http-server"
	mock_pid=""

	printf '[smoke] running scenario: %s\n' "$scenario"
	setup_legacy_project_bridge

	go build -o "$mock_server_bin" "$smoke_root/bin/mock-http-server-main.go" || fail "building mock HTTP server"

	cleanup_mock_server() {
		if [ -n "$mock_pid" ]; then
			kill "$mock_pid" 2>/dev/null || true
			wait "$mock_pid" 2>/dev/null || true
		fi
	}

	"$mock_server_bin" "$mock_port" "hello from mock http" >"$run_dir/mock-http-server.log" 2>&1 &
	mock_pid=$!
	trap 'cleanup_mock_server; cleanup' EXIT HUP INT TERM
	sleep 1

	cat >"$http_config" <<EOF
kitchens:
  http-echo:
    http_client:
      base_url: http://localhost:$mock_port/v1
      auth_key: ""
      auth_type: bearer
      model: test-model
      response_format: openai
    stations: [echo]
    cost_tier: free
routing:
  keywords:
    echo: http-echo
  default: http-echo
ledger:
  ndjson: $run_dir/http-ledger.ndjson
  db: $run_dir/http-milliways.db
EOF

	"$milliways_bin" \
		-c "$http_config" \
		--project-root "$scenario_project_root" \
		--use-legacy-conversation \
		--timeout 15s \
		"echo hello" \
		>"$log" 2>&1
	rc=$?

	cleanup_mock_server
	trap cleanup EXIT HUP INT TERM

	failures=0
	if [ "$rc" -ne 0 ]; then
		printf '[smoke] FAIL: %s expected exit 0, got %d\n' "$scenario" "$rc" >&2
		failures=$((failures + 1))
	fi

	if ! tr '\n' ' ' <"$log" | tr -s ' ' | grep -Fq -- "hello from mock http"; then
		printf '[smoke] FAIL: %s missing expected output: hello from mock http\n' "$scenario" >&2
		failures=$((failures + 1))
	fi

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
run_scenario_user_switch || overall=1
run_scenario_tam_10_6 || overall=1
run_scenario_tam_10_2 || overall=1
run_scenario_kp22_2 || overall=1
run_scenario_tp9_4 || overall=1
run_scenario_tp9_5 || overall=1
run_scenario_hk5_2 || overall=1

if [ "$overall" -eq 0 ]; then
	printf '[smoke] all scenarios passed\n'
else
	printf '[smoke] one or more scenarios failed\n' >&2
fi

exit "$overall"

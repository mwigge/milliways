#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SMOKE_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
TMPDIR="${TMPDIR:-/tmp}"
BIN="$TMPDIR/milliways-smoke-$$"
BUNDLE_JSON="$(mktemp "${TMPDIR}/milliways-context-bundle-$$.json")"
RUN_DIR="$(mktemp -d "${TMPDIR}/milliways-context-run-$$.XXXXXX")"
FAKE_KITCHEN="$RUN_DIR/fake-opencode.sh"
CONFIG_PATH="$RUN_DIR/carte.yaml"
PROJECT_ROOT="$RUN_DIR/project"
HOME_DIR="$RUN_DIR/home"
XDG_CONFIG_HOME="$RUN_DIR/xdg"

cleanup() {
	rm -f "$BIN" "$BUNDLE_JSON"
	rm -rf "$RUN_DIR"
}
trap cleanup EXIT

# Build milliways
go build -o "$BIN" "$SMOKE_ROOT/cmd/milliways" 2>/dev/null || {
	echo "build failed"
	exit 1
}

mkdir -p "$PROJECT_ROOT/.git" "$PROJECT_ROOT/.codegraph" "$HOME_DIR" "$XDG_CONFIG_HOME"

cat >"$FAKE_KITCHEN" <<'KITCHEN_EOF'
#!/bin/sh
printf '%s\n' 'context stdin ok'
KITCHEN_EOF
chmod +x "$FAKE_KITCHEN"

cat >"$CONFIG_PATH" <<CONFIG_EOF
kitchens:
  opencode:
    cmd: "$FAKE_KITCHEN"
    enabled: true
routing:
  default: opencode
CONFIG_EOF

# Write representative JSON bundle
cat >"$BUNDLE_JSON" <<'BUNDLE_EOF'
{
  "schema_version": "1",
  "collected_at": "2026-04-19T12:00:00Z",
  "collectors": {
    "editor": {
      "buffer": {
        "path": "/tmp/fake.go",
        "filetype": "go",
        "modified": false,
        "lines": ["package main", "", "func main() {}", "", "// end"],
        "total_lines": 5,
        "visible_start": 1,
        "visible_end": 5,
        "visible_range": { "start": 1, "end": 5 }
      },
      "cursor": { "line": 3, "column": 10 },
      "lsp": {
        "errors": 0,
        "warnings": 1,
        "diagnostics": [
          { "severity": "warning", "message": "unused variable", "line": 3 }
        ]
      },
      "git": {
        "branch": "main",
        "dirty": false,
        "files_changed": 0,
        "ahead": 0,
        "behind": 0
      },
      "project": {
        "root": "/tmp/project",
        "language": "go",
        "primary_language": "go",
        "open_buffers": [
          { "path": "/tmp/fake.go", "filetype": "go" }
        ],
        "recent_files": ["/tmp/fake.go"]
      }
    }
  }
  ,"total_bytes": 256
}
BUNDLE_EOF

# Invoke milliways with --context-stdin
# We expect it to at minimum parse the bundle without error.
# It may fail to connect to a server, but the flag/bundle parsing must succeed.
output=$(HOME="$HOME_DIR" XDG_CONFIG_HOME="$XDG_CONFIG_HOME" MILLIWAYS_MEMPALACE_MCP_CMD="$FAKE_KITCHEN" "$BIN" \
	--config "$CONFIG_PATH" \
	--project-root "$PROJECT_ROOT" \
	--use-legacy-conversation \
	--context-stdin \
	"explain the auth flow" <"$BUNDLE_JSON" 2>&1) || true

# Check: no "error" or "invalid" in output (bundle parsing errors)
if echo "$output" | grep -qi "error\|invalid"; then
	echo "bundle parsing produced error: $output"
	exit 1
fi

echo "nvim-context smoke: bundle parsed OK"
exit 0

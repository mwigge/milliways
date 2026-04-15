#!/bin/sh
# Mock claude binary that emits stream-json events.
# Ignores all arguments (--print, --verbose, etc.) and stdin.
# Outputs a minimal stream-json session.
cat <<'EVENTS'
{"type":"system","subtype":"init","session_id":"test-session-123","model":"claude-opus-4-6"}
{"type":"assistant","message":{"content":[{"type":"text","text":"Here is a hello world:\n```go\npackage main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n```\nDone."}]}}
{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","resetsAt":0}}
{"type":"result","total_cost_usd":0.05,"duration_ms":1200,"num_turns":1,"is_error":false,"usage":{"input_tokens":100,"output_tokens":50,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}
EVENTS

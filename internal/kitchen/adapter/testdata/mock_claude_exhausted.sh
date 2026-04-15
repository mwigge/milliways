#!/bin/sh
# Mock claude that reports rate limit exhaustion
cat <<'EVENTS'
{"type":"system","subtype":"init","session_id":"test-exhaust-456","model":"claude-opus-4-6"}
{"type":"rate_limit_event","rate_limit_info":{"status":"exhausted","resetsAt":1893456000}}
{"type":"result","total_cost_usd":0,"duration_ms":100,"num_turns":0,"is_error":true,"usage":{"input_tokens":0,"output_tokens":0}}
EVENTS
exit 1

#!/usr/bin/env bash
# Read a PNG file and emit a kitty graphics protocol escape sequence
# to stdout. The receiving terminal decides whether to render it.
# Used by SPIKE-wezterm-overlay-kitty-graphics.md (test 1, the pane control).
set -euo pipefail

if [[ $# -lt 1 ]]; then
    echo "usage: $0 <png-file>" >&2
    exit 1
fi

PNG="$1"
if [[ ! -f "$PNG" ]]; then
    echo "error: $PNG not found" >&2
    exit 1
fi

# a=T  : transmit and display
# f=100: data is PNG
# t=d  : transmission medium = direct (inline base64)
# m=0  : final chunk (we send everything in one frame)
B64="$(base64 < "$PNG" | tr -d '\n')"
printf '\x1b_Ga=T,f=100,t=d,m=0;%s\x1b\\' "$B64"
printf '\n'

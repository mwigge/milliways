#!/usr/bin/env bash
# Generate a 200x100 PNG with a recognisable solid red rectangle and
# write it to stdout. Used by SPIKE-wezterm-overlay-kitty-graphics.md.
set -euo pipefail

if command -v magick >/dev/null 2>&1; then
    magick -size 200x100 xc:'#cc0033' png:-
elif command -v convert >/dev/null 2>&1; then
    convert -size 200x100 xc:'#cc0033' png:-
elif command -v python3 >/dev/null 2>&1; then
    python3 - <<'PY'
import struct, zlib, sys

W, H = 200, 100
R, G, B = 0xCC, 0x00, 0x33

def chunk(tag, data):
    crc = zlib.crc32(tag + data)
    return struct.pack('>I', len(data)) + tag + data + struct.pack('>I', crc)

raw = bytearray()
row = bytes([R, G, B] * W)
for _ in range(H):
    raw.append(0)              # filter byte: none
    raw.extend(row)

ihdr = struct.pack('>IIBBBBB', W, H, 8, 2, 0, 0, 0)  # 8-bit RGB
idat = zlib.compress(bytes(raw), 9)

png = b'\x89PNG\r\n\x1a\n'
png += chunk(b'IHDR', ihdr)
png += chunk(b'IDAT', idat)
png += chunk(b'IEND', b'')

sys.stdout.buffer.write(png)
PY
else
    echo "error: need 'magick', 'convert', or 'python3' to generate the test PNG." >&2
    exit 1
fi

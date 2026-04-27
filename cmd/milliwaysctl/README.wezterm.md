Wezterm integration example

Place milliways.lua in your ~/.wezterm.lua (or require it) to enable a
simple status bar that starts a background `milliwaysctl status --watch`
sidecar and reads the atomic `${state}/status.cur` file produced by the
sidecar.

Steps:
1. Copy cmd/milliwaysctl/milliways.lua into your wezterm config or require it.
2. Ensure milliwaysctl is on PATH and milliwaysd is running (socket in XDG_RUNTIME_DIR or ~/.local/state/milliways/sock).
3. Restart wezterm. The sidecar is started lazily on the first update-status event.

Notes and best practices:
- The sidecar writes state/status.cur atomically (tmp+fsync+rename). The
  Lua config reads it for display. For older systems without XDG_RUNTIME_DIR
  set, the default state path is ~/.local/state/milliways.
- The example is intentionally small — production usage should check that
  status.cur mtime is recent and fall back to a direct RPC call otherwise.
- The watch sidecar ensures it exits if the parent process dies (ppid==1
  heuristic). For stricter lifecycle, consider PDEATHSIG on Linux.

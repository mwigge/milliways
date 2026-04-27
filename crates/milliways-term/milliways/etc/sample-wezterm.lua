-- Sample milliways-aware wezterm config.
--
-- Drop this file into ~/.config/wezterm/wezterm.lua (or symlink it from
-- there) to get a milliways cockpit out of the box: right-status bar
-- driven by `milliwaysctl status --json`, plus the four cockpit
-- keybindings (Cmd+Shift+A/C/G/O).
--
-- Install steps:
--
--   1. Run `./install.sh` from the milliways repo root. This builds
--      milliways/milliwaysd/milliwaysctl/milliways-term and copies
--      `crates/milliways-term/milliways/etc/milliways.lua` to
--      `~/.local/share/milliways/milliways.lua` so `require('milliways')`
--      can find it.
--
--   2. Either copy this file to `~/.config/wezterm/wezterm.lua`, or
--      symlink it:
--
--        mkdir -p ~/.config/wezterm
--        ln -s "$PWD/crates/milliways-term/milliways/etc/sample-wezterm.lua" \
--              ~/.config/wezterm/wezterm.lua
--
--   3. Make sure `milliwaysd` is running (the status bar polls it once
--      per second and the agent picker queries it on demand):
--
--        milliwaysd &
--
--   4. Launch the cockpit terminal:
--
--        milliways-term
--
-- The `package.path` line below ensures wezterm's Lua loader can find
-- `milliways.lua` in `~/.local/share/milliways/`. If you installed
-- elsewhere, adjust the path or symlink `milliways.lua` next to this
-- file and remove the `package.path` line.

package.path = package.path
  .. ';' .. (os.getenv('HOME') or '') .. '/.local/share/milliways/?.lua'

local wezterm = require 'wezterm'
local milliways = require 'milliways'  -- the file alongside this one
local config = wezterm.config_builder()

-- Theme matching the cockpit semantic palette (tokyonight_storm pairs
-- well with milliways.theme = { ok, warn, err, accent, dim }).
config.color_scheme = 'tokyonight_storm'
config.font = wezterm.font_with_fallback({
  'JetBrains Mono',
  'Symbols Nerd Font Mono',
})
config.font_size = 14.0

-- Apply milliways cockpit (status bar + keybindings).
milliways.apply(config)

return config

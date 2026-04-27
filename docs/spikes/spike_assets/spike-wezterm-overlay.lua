-- spike-wezterm-overlay.lua
-- Minimal wezterm config used to test whether the overlay surface
-- consumes kitty graphics protocol escapes.
--
-- Used by docs/spikes/SPIKE-wezterm-overlay-kitty-graphics.md.

local wezterm = require 'wezterm'
local act = wezterm.action

local config = wezterm.config_builder()

-- Read the test PNG once at config load.
local png_path = '/tmp/spike-test.png'
local png_bytes = ''
do
  local f = io.open(png_path, 'rb')
  if f then
    png_bytes = f:read('*all')
    f:close()
  end
end

-- Encode the PNG as a kitty-graphics protocol escape.
-- Format: ESC _ G a=T,f=100,t=d,m=0;BASE64 ESC \
local function kitty_image_escape(bytes)
  if bytes == '' then
    return '[no test PNG at ' .. png_path .. ']'
  end
  local b64 = wezterm.encode_base64(bytes)
  return '\x1b_Ga=T,f=100,t=d,m=0;' .. b64 .. '\x1b\\'
end

local IMG = kitty_image_escape(png_bytes)

-- ----------------------------------------------------------------------
-- Test 2: inject an entry containing the kitty-graphics escape into the
-- CommandPalette. If wezterm's overlay surface renders kitty graphics,
-- the PNG will appear inside the palette dropdown.
-- ----------------------------------------------------------------------
wezterm.on('augment-command-palette', function(window, pane)
  return {
    {
      brief = 'milliways-spike: kitty graphics in palette ' .. IMG,
      action = act.SendString('palette spike triggered\n'),
    },
    {
      brief = 'milliways-spike: open custom overlay (test 3)',
      action = act.EmitEvent 'milliways-spike-overlay',
    },
  }
end)

-- ----------------------------------------------------------------------
-- Test 3: a custom overlay opened via InputSelector. The choice labels
-- contain kitty-graphics escapes; same render question as test 2.
-- ----------------------------------------------------------------------
wezterm.on('milliways-spike-overlay', function(window, pane)
  window:perform_action(
    act.InputSelector {
      title = 'milliways-spike: kitty graphics in custom overlay',
      choices = {
        { label = 'choice A ' .. IMG, id = 'a' },
        { label = 'choice B (text only)', id = 'b' },
        { label = 'choice C ' .. IMG, id = 'c' },
      },
      action = wezterm.action_callback(function(window, pane, id, label)
        if id then
          window:active_pane():send_text('selected ' .. id .. '\n')
        end
      end),
    },
    pane
  )
end)

-- Bind Ctrl+Shift+M to test 3.
config.keys = {
  {
    key = 'm',
    mods = 'CTRL|SHIFT',
    action = act.EmitEvent 'milliways-spike-overlay',
  },
}

return config

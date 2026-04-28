-- milliways wezterm integration
--
-- Default program: milliways (the AI terminal). Every new tab opens milliways.
-- Status bar reads ${state}/observe.cur written by milliwaysctl observe --watch
--
-- Layout: [⚡ woke Xm ago] [≈≈ MW v0.x] [~/path] [●claude] [1:C 2:X 3:G 4:M 5:L]
-- Wake badge appears for 5 min after system resumes from sleep.
--
-- Leader key: Ctrl+Space
--   Leader + a         open milliways agent pane (split below)
--   Leader + 1..4      switch active runner via milliwaysctl open
--   Leader + r         resume modal — shows wake/session summary, re-opens last agent
--   Leader + k         context overlay
--   Leader + w         observe-render overlay (metrics/spans)
--   Leader + z         open a plain shell tab (escape hatch)

local wezterm = require 'wezterm'
local act     = wezterm.action
local io      = io
local os      = os

local config = wezterm.config_builder and wezterm.config_builder() or {}

-- Suppress domain entries from the Shell menu.
-- WezTerm auto-detects SSH hosts from ~/.ssh/config and adds "New Tab (Domain SSH:*)"
-- entries for every host. For milliways we don't use SSH domains — clear them so
-- the Shell menu only shows the built-in window/pane actions.
config.ssh_domains  = {}
config.unix_domains = {}

-- ── Appearance ──────────────────────────────────────────────────────────────

-- Black background, phosphor green text — matches the terminal's own color palette.
config.colors = {
  foreground = '#4db51f',   -- phosphor green
  background = '#000000',
  cursor_bg  = '#4db51f',
  cursor_fg  = '#000000',
  selection_bg = '#1a3d0a',
  selection_fg = '#80d040',
  ansi = {
    '#1a1a1a', '#cc2222', '#4db51f', '#9a8a00',
    '#1a6abf', '#7a3faa', '#1a9090', '#b0b0a0',
  },
  brights = {
    '#3a3a3a', '#ee4444', '#80d040', '#cfc000',
    '#4499ee', '#aa66dd', '#22bbbb', '#e8e8d8',
  },
}
config.font               = wezterm.font('JetBrains Mono', { weight = 'Regular' })
config.font_size          = 13.0
config.window_decorations = 'TITLE | RESIZE'
config.hide_tab_bar_if_only_one_tab = false
config.use_fancy_tab_bar  = false
config.tab_bar_at_bottom  = true

-- Ensure ~/.local/bin is in PATH so wezterm can find milliways and milliwaysctl.
local local_bin = (os.getenv('HOME') or '') .. '/.local/bin'
local path_env  = os.getenv('PATH') or '/usr/bin:/bin:/usr/sbin:/sbin'
if not path_env:find(local_bin, 1, true) then
  path_env = local_bin .. ':' .. path_env
end
config.set_environment_variables = { PATH = path_env }

-- Every new tab/pane runs milliways.
-- MILLIWAYS_REPL=1 tells the launcher to skip the milliways-term exec and
-- drop straight into the built-in terminal mode.
config.set_environment_variables.MILLIWAYS_REPL = '1'
config.default_prog = { local_bin .. '/milliways' }

-- ── State paths ──────────────────────────────────────────────────────────────

local home      = os.getenv('HOME') or ''
local xdg       = os.getenv('XDG_RUNTIME_DIR') or ''
local state_dir = (xdg ~= '' and xdg .. '/milliways') or (home .. '/.local/state/milliways')
local observe_cur = state_dir .. '/observe.cur'

-- ── Status bar ───────────────────────────────────────────────────────────────

local abbrs = {
  claude  = 'C',
  codex   = 'X',
  copilot = 'G',
  minimax = 'M',
  ['local'] = 'L',
}

local function abbrev_path(path)
  if home ~= '' and path:sub(1, #home) == home then
    return '~' .. path:sub(#home + 1)
  end
  return path
end

local function read_observe()
  local f = io.open(observe_cur, 'r')
  if not f then return nil end
  local raw = f:read('*a')
  f:close()
  if not raw or raw == '' then return nil end
  local ok, data = pcall(wezterm.json_parse, raw)
  if not ok or not data then return nil end
  return data
end

wezterm.on('update-status', function(window, _pane)
  local data = read_observe()
  if not data then
    window:set_right_status(wezterm.format({
      { Foreground = { AnsiColor = 'Grey' } },
      { Text = ' milliways: daemon not running ' },
    }))
    return
  end

  local ver      = data.v or '?'
  local cwd      = abbrev_path(data.p or '')
  local current  = data.c or ''
  local agents   = data.a or {}
  local woke_ago = data.woke_ago  -- seconds since wake, or nil

  local cells = {
    { Background = { Color = '#1d2021' } },
  }

  -- Wake badge: ⚡ Xm when system just resumed from sleep.
  if woke_ago then
    local mins = math.floor(woke_ago / 60)
    local label = mins > 0 and (mins .. 'm') or '<1m'
    table.insert(cells, { Foreground = { Color = '#fe8019' } })
    table.insert(cells, { Text = ' ⚡ woke ' .. label .. ' ago ' })
    table.insert(cells, { Foreground = { Color = '#504945' } })
    table.insert(cells, { Text = '│' })
  end

  table.insert(cells, { Foreground = { Color = '#83a598' } })
  table.insert(cells, { Text = ' ≈≈ MW ' .. ver .. ' ' })
  table.insert(cells, { Foreground = { Color = '#504945' } })
  table.insert(cells, { Text = '│' })
  table.insert(cells, { Foreground = { Color = '#a89984' } })
  table.insert(cells, { Text = ' ' .. cwd .. ' ' })
  table.insert(cells, { Foreground = { Color = '#504945' } })
  table.insert(cells, { Text = '│' })

  if current ~= '' then
    table.insert(cells, { Foreground = { Color = '#b8bb26' } })
    table.insert(cells, { Text = ' ●' .. current .. ' ' })
  else
    table.insert(cells, { Foreground = { Color = '#504945' } })
    table.insert(cells, { Text = ' ○— ' })
  end

  table.insert(cells, { Foreground = { Color = '#504945' } })
  table.insert(cells, { Text = '│' })

  for i, agent in ipairs(agents) do
    local abbr = abbrs[agent] or agent:sub(1, 1):upper()
    local fg   = (agent == current) and '#b8bb26' or '#7c6f64'
    table.insert(cells, { Foreground = { Color = fg } })
    table.insert(cells, { Text = ' ' .. i .. ':' .. abbr })
  end
  table.insert(cells, { Text = ' ' })

  window:set_right_status(wezterm.format(cells))
end)

-- ── Leader keybindings ───────────────────────────────────────────────────────

config.leader = { key = 'Space', mods = 'CTRL', timeout_milliseconds = 1500 }

config.keys = {
  -- Leader + a  →  open milliways pane split below
  {
    key = 'a', mods = 'LEADER',
    action = act.SplitPane {
      direction = 'Down',
      size = { Percent = 40 },
      command = { args = { local_bin .. '/milliways', '--repl' } },
    },
  },
  -- Leader + z  →  plain shell tab (escape hatch)
  {
    key = 'z', mods = 'LEADER',
    action = act.SpawnCommandInNewTab { args = { os.getenv('SHELL') or 'zsh' } },
  },
  -- Leader + 1..4  →  switch active runner
  {
    key = '1', mods = 'LEADER',
    action = act.SpawnCommandInNewTab { args = { 'milliwaysctl', 'open', '--agent', 'claude' } },
  },
  {
    key = '2', mods = 'LEADER',
    action = act.SpawnCommandInNewTab { args = { 'milliwaysctl', 'open', '--agent', 'codex' } },
  },
  {
    key = '3', mods = 'LEADER',
    action = act.SpawnCommandInNewTab { args = { 'milliwaysctl', 'open', '--agent', 'copilot' } },
  },
  {
    key = '4', mods = 'LEADER',
    action = act.SpawnCommandInNewTab { args = { 'milliwaysctl', 'open', '--agent', 'minimax' } },
  },
  -- Leader + r  →  resume modal after sleep/wake
  {
    key = 'r', mods = 'LEADER',
    action = wezterm.action_callback(function(window, pane)
      local data   = read_observe()
      local cur    = (data and data.c) or ''
      local woke   = data and data.woke_ago

      local header = '≈≈ milliways resume'
      if woke then
        local mins = math.floor(woke / 60)
        local ago  = mins > 0 and (mins .. 'm') or '<1m'
        header = '⚡ woke ' .. ago .. ' ago'
      end

      local desc
      if cur ~= '' then
        desc = header .. ' · last agent: ' .. cur .. ' · press Enter to reopen, Esc to cancel'
      else
        desc = header .. ' · no active session · press Esc'
      end

      window:perform_action(
        act.PromptInputLine {
          description = wezterm.format({
            { Attribute = { Intensity = 'Bold' } },
            { Foreground = { Color = '#fe8019' } },
            { Text = desc },
          }),
          action = wezterm.action_callback(function(win, _, line)
            -- line == '' means Enter with no text (confirmed), nil means Esc
            if line ~= nil and cur ~= '' then
              win:perform_action(
                act.SpawnCommandInNewTab { args = { 'milliwaysctl', 'open', '--agent', cur } },
                pane
              )
            end
          end),
        },
        pane
      )
    end),
  },
  -- Leader + k  →  context overlay
  {
    key = 'k', mods = 'LEADER',
    action = act.SpawnCommandInNewTab { args = { 'milliwaysctl', 'context', '--all' } },
  },
  -- Leader + w  →  observability render overlay
  {
    key = 'w', mods = 'LEADER',
    action = act.SpawnCommandInNewTab { args = { 'milliwaysctl', 'observe-render' } },
  },
}

-- ── Auto-start milliwaysd when wezterm opens ─────────────────────────────────
-- Spawns the daemon once; subsequent windows reuse the existing socket.

wezterm.on('gui-startup', function(_cmd)
  local daemon_sock = state_dir .. '/sock'
  local f = io.open(daemon_sock, 'r')
  if f then
    f:close()
  else
    wezterm.background_child_process({ local_bin .. '/milliwaysd' })
  end
  wezterm.background_child_process({ local_bin .. '/milliwaysctl', 'observe', '--watch' })
end)

return config

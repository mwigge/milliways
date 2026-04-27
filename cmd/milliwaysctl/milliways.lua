-- milliways wezterm integration
-- Reads ${stateDir}/observe.cur (written by milliwaysctl observe --watch) and renders
-- the MW status bar. Format of observe.cur:
--   {"v":"0.1.0","p":"/home/user/project","c":"claude","a":["claude","codex","copilot","minimax"]}
--
-- Status bar layout: [≈≈ MW v0.x] [~/path] [●claude] [1:C 2:X 3:G 4:M 5:L]
-- Agent key numbers are derived from the order of agents in the "a" array.
-- Key 5 is always "local". Other keys match array indices.
local wezterm = require 'wezterm'
local io = io
local os = os

local state_dir = os.getenv('XDG_RUNTIME_DIR') and (os.getenv('XDG_RUNTIME_DIR') .. '/milliways') or (os.getenv('HOME') .. '/.local/state/milliways')
local sock = state_dir .. '/sock'
local observe_cur = state_dir .. '/observe.cur'

-- Abbreviation map for known agents.
local abbrs = {
  claude  = 'C',
  codex   = 'X',
  copilot = 'G',
  minimax = 'M',
  local   = 'L',
}

local function abbreviate_path(path)
  local home = os.getenv('HOME') or ''
  if home ~= '' and path:sub(1, #home) == home then
    return '~' .. path:sub(#home + 1)
  end
  return path
end

local function render_status()
  local f = io.open(observe_cur, 'r')
  if not f then
    return nil
  end
  local content = f:read('*a')
  f:close()
  if content == '' then
    return nil
  end

  local ok, data = pcall(require('json').decode, content)
  if not ok or not data then
    return nil
  end

  local version = data.v or '0.0.0'
  local cwd = data.p or ''
  local current = data.c or ''
  local agents = data.a or {}

  local mw = '≈≈ MW ' .. version
  local path = abbreviate_path(cwd)

  local agent_badge = ''
  if current ~= '' then
    agent_badge = ' ●' .. current
  else
    agent_badge = ' ○—'
  end

  -- Build hotkey hints: keys 1..N from agents list, key 5 = local.
  local hints = ''
  for i, agent in ipairs(agents) do
    local abbr = abbrs[agent] or string.sub(agent, 1, 1)
    hints = hints .. ' ' .. i .. ':' .. abbr
  end
  -- Always append key 5 for local.
  hints = hints .. ' 5:L'

  return mw .. ' │ ' .. path .. ' │' .. agent_badge .. ' │' .. hints
end

wezterm.on('update-status', function(window, pane)
  local text = render_status()
  if text then
    wezterm.set_right_status(text)
  else
    wezterm.set_right_status('milliways: ready')
  end
end)

-- Handle key events to switch agents via number keys.
-- Agents 1-4 come from the observe.cur agent list; 5 is always local.
wezterm.on('activate-agent', function(window, pane, agent_id)
  window:perform_action(wezterm.action{SpawnCommandInNewTab = {
    args = {'milliwaysctl', 'open', '--agent', agent_id},
    cwd = pane:get_currentWorkingDirectory(),
  }}, pane)
end)
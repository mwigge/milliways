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
--   Leader + /         milliwaysctl slash dispatcher — type `/<verb> [args...]`
--                      runs `milliwaysctl <verb> [args...]` in a new tab
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

-- Every new tab/pane runs the user's shell. Agent surfaces are explicit:
-- - Leader + 1..4   open a runner via `milliwaysctl open --agent <name>`
-- - Leader + a      same for the default agent (claude)
-- - Leader + /      slash-command palette → milliwaysctl <verb> [args...]
--
-- The legacy default_prog = milliways pattern was removed when --repl /
-- MILLIWAYS_REPL=1 was deleted. Setting default_prog to milliways with
-- the launcher's modeCockpit dispatch in place would recursively syscall
-- exec milliways-term inside every new tab.
config.default_prog = { os.getenv('SHELL') or '/bin/zsh' }

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
  -- Leader + a  →  open the default (claude) agent pane split below
  {
    key = 'a', mods = 'LEADER',
    action = act.SplitPane {
      direction = 'Down',
      size = { Percent = 40 },
      command = { args = { 'milliwaysctl', 'open', '--agent', 'claude' } },
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
  -- Leader + /  →  milliwaysctl command palette
  --
  -- Opens an InputSelector overlay populated with curated ctl invocations.
  -- - Type to fuzzy-filter the list (wezterm's built-in fuzzy mode).
  -- - Pick a complete verb → dispatched immediately in a new tab.
  -- - Pick a verb that takes args (trailing space in id) → falls through
  --   to a PromptInputLine prefilled with that verb so you fill in the rest.
  -- - Pick "free-form" → opens an empty PromptInputLine for arbitrary input
  --   (e.g., to dispatch a ctl subcommand not in the curated list).
  --
  -- Adding a new ctl subcommand requires updating ctl_choices below to make
  -- it discoverable, but ANY ctl subcommand stays callable via the free-form
  -- escape hatch — the dispatcher itself stays generic.
  {
    key = '/', mods = 'LEADER',
    action = wezterm.action_callback(function(window, pane)
      window:perform_action(open_ctl_palette(), pane)
    end),
  },
}

-- ── ctl palette helpers ─────────────────────────────────────────────────────

-- ctl_choices lists the curated commands surfaced in the Leader + / palette.
-- An id ending in a single space tells the picker "this verb takes args";
-- the picker then opens a PromptInputLine prefilled with that prefix so the
-- user can complete the invocation.
local ctl_choices = {
  -- Discovery
  { label = 'agents                    list registered agents',                    id = 'agents' },
  { label = 'quota                     show quota snapshots',                      id = 'quota' },
  { label = 'status                    fetch live cockpit state',                  id = 'status' },
  { label = 'routing                   peek recent sommelier decisions',           id = 'routing' },
  { label = 'spans                     recent OTel spans',                         id = 'spans' },
  -- OpenSpec (opsx) — thin shell over the openspec CLI
  { label = '/opsx-list                list openspec changes',                     id = 'opsx list' },
  { label = '/opsx-status …            show change progress',                     id = 'opsx status ' },
  { label = '/opsx-show …              show full change detail',                  id = 'opsx show ' },
  { label = '/opsx-archive …           archive a completed change',               id = 'opsx archive ' },
  { label = '/opsx-validate …          validate a change',                        id = 'opsx validate ' },
  -- Local-model bootstrap (slash-command alias on the left, ctl invocation on the right)
  { label = '/list-local-models        show models served by the active backend',  id = 'local list-models' },
  { label = '/install-local-server     install llama.cpp + default coder model',   id = 'local install-server' },
  { label = '/install-local-swap       install llama-swap (hot model swap)',       id = 'local install-swap' },
  { label = '/switch-local-server …    pick backend (llama-server | ollama | …)',  id = 'local switch-server ' },
  { label = '/download-local-model …   fetch a GGUF from HuggingFace',             id = 'local download-model ' },
  { label = '/setup-local-model …      download + register in llama-swap.yaml',    id = 'local setup-model ' },
  -- Free-form escape hatch (kept last so casual fuzzy-typing finds curated entries first)
  { label = '… free-form milliwaysctl invocation …',                                id = '__free_form__' },
}

-- dispatch_ctl_args takes the parsed argv (without the leading "milliwaysctl")
-- and spawns it in a new tab. Returns nil if argv is empty.
function dispatch_ctl_args(window, pane, argv)
  if #argv == 0 then return end
  local args = { 'milliwaysctl' }
  for _, w in ipairs(argv) do
    table.insert(args, w)
  end
  window:perform_action(act.SpawnCommandInNewTab { args = args }, pane)
end

-- split_ws splits a string on runs of whitespace (no quoting support;
-- values containing spaces aren't expressible from this dispatcher).
function split_ws(s)
  local out = {}
  for word in s:gmatch('%S+') do
    table.insert(out, word)
  end
  return out
end

-- open_ctl_palette returns the InputSelector action for Leader + /.
function open_ctl_palette()
  return act.InputSelector {
    title = 'milliwaysctl',
    fuzzy = true,
    description = 'Pick a command (Esc to cancel, type to filter)',
    fuzzy_description = 'milliwaysctl ▸ ',
    choices = ctl_choices,
    action = wezterm.action_callback(function(win, pn, id, _)
      -- Cancel: id and label are both nil per docs.
      if id == nil then return end

      -- Free-form escape hatch → empty PromptInputLine.
      if id == '__free_form__' then
        win:perform_action(open_ctl_prompt(''), pn)
        return
      end

      -- Verb that takes args (trailing space in id) → prefilled prompt.
      if id:sub(-1) == ' ' then
        win:perform_action(open_ctl_prompt(id), pn)
        return
      end

      -- Complete verb → dispatch immediately.
      dispatch_ctl_args(win, pn, split_ws(id))
    end),
  }
end

-- open_ctl_prompt returns a PromptInputLine action that accepts free-form
-- text and dispatches it as `milliwaysctl <args...>`. `initial` is prefilled
-- text; pass '' for a blank prompt.
function open_ctl_prompt(initial)
  return act.PromptInputLine {
    description = wezterm.format({
      { Attribute = { Intensity = 'Bold' } },
      { Foreground = { Color = '#4db51f' } },
      { Text = 'milliwaysctl ▸ ' },
    }),
    initial_value = initial,
    action = wezterm.action_callback(function(win, pn, line)
      -- nil = Esc; '' = Enter on empty. Both bail.
      if line == nil or line == '' then return end
      -- Strip a single leading '/' so `/local list-models` and
      -- `local list-models` both work.
      if line:sub(1, 1) == '/' then line = line:sub(2) end
      dispatch_ctl_args(win, pn, split_ws(line))
    end),
  }
end

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

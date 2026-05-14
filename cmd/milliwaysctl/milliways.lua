-- milliways wezterm integration
--
-- Default program: milliways (the AI terminal). Every new tab opens milliways.
-- Header status reads ${state}/observe.cur written by milliwaysctl observe --watch
--
-- Layout: [⚡ woke Xm ago] [≈≈ MW v0.x] [~/path] [●claude] [1:C 2:X 3:Cp 4:M 5:G 6:L 7:P]
-- Wake badge appears for 5 min after system resumes from sleep.
--
-- Leader key: Ctrl+Space
--   Leader + a         open milliways agent pane (split below)
--   Leader + 1..7      switch active runner via milliwaysctl open
--   Leader + r         resume modal — shows wake/session summary, re-opens last agent
--   Leader + k         context overlay
--   Leader + w         observe-render overlay (metrics/spans)
--   Leader + /         milliwaysctl slash dispatcher — type `/<verb> [args...]`
--                      runs `milliwaysctl <verb> [args...]` in a new tab
--   Leader + z         open a plain shell tab (escape hatch)

local wezterm = require 'wezterm'
local act     = wezterm.action
local mux     = wezterm.mux
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
config.font_size          = 12.0
config.window_decorations = 'TITLE | RESIZE'
config.hide_tab_bar_if_only_one_tab = false
config.use_fancy_tab_bar  = false
config.tab_bar_at_bottom  = false
config.status_update_interval = 500
config.window_padding = { left = 6, right = 6, top = 4, bottom = 4 }
config.check_for_updates = false
config.bypass_mouse_reporting_modifiers = 'SHIFT'

-- ── Clickable URLs ────────────────────────────────────────────────────────────
-- wezterm detects these patterns and makes them Ctrl+Click (macOS: Cmd+Click)
-- openable. The first matching rule wins, so most-specific rules go first.
config.hyperlink_rules = {
  -- Standard https / http URLs
  { regex = '\\b(https?://[\\w\\-@:%.+~#=/?&]+)', highlight = 1, format = '$1' },
  -- Project issue / PR shorthand
  { regex = '\\b(?:issue|issues|pr|PR)[: ]#?(\\d+)\\b', highlight = 1,
    format = 'https://github.com/mwigge/milliways/issues/$1' },
  -- File paths with line numbers
  { regex = '(~/[\\w\\-./]+|/[\\w\\-./]+):(\\d+)\\b', highlight = 1, format = 'file://$1:$2' },
  -- File paths: absolute and home-relative
  { regex = '(~/[\\w\\-./]+|/[\\w\\-./]+\\.[a-zA-Z0-9]+)', highlight = 1, format = 'file://$1' },
  -- GitHub short refs: owner/repo#123  owner/repo@sha
  { regex = '\\b([a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+[#@][a-zA-Z0-9]+)\\b',
    highlight = 1, format = 'https://github.com/$1' },
  -- Go package paths: github.com/owner/repo/pkg
  { regex = '\\b(github\\.com/[\\w\\-./]+)\\b', highlight = 1, format = 'https://$1' },
  -- Bare issue / PR numbers (#123)
  { regex = '#(\\d+)\\b', highlight = 0,
    format = 'https://github.com/mwigge/milliways/issues/$1' },
}

-- Open clicked links with the system default browser / editor.
-- Ctrl+Click (macOS: Cmd+Click) opens the hyperlink under the cursor.
config.mouse_bindings = {
  {
    event  = { Up = { streak = 1, button = 'Left' } },
    mods   = 'CTRL',
    action = wezterm.action.OpenLinkAtMouseCursor,
  },
}

-- Resolve MilliWays binaries. Linux package installs place binaries in /usr/bin,
-- while binary/source installs use ~/.local/bin. Prefer the managed package
-- path when present so stale user-local binaries cannot shadow package upgrades.
local local_bin = (os.getenv('HOME') or '') .. '/.local/bin'
local path_env  = os.getenv('PATH') or '/usr/bin:/bin:/usr/sbin:/sbin'
local app_bin   = wezterm.executable_dir

local function file_exists(path)
  local f = io.open(path, 'r')
  if f then
    f:close()
    return true
  end
  return false
end

local function resolve_milliways_bin(name)
  local override = os.getenv('MILLIWAYS_BIN_DIR')
  if override and override ~= '' and file_exists(override .. '/' .. name) then
    return override .. '/' .. name
  end
  if app_bin and app_bin ~= '' and file_exists(app_bin .. '/' .. name) then
    return app_bin .. '/' .. name
  end
  if file_exists('/usr/bin/' .. name) then
    return '/usr/bin/' .. name
  end
  if file_exists(local_bin .. '/' .. name) then
    return local_bin .. '/' .. name
  end
  return name
end

local mw_bin      = resolve_milliways_bin('milliways')
local mwctl_bin   = resolve_milliways_bin('milliwaysctl')
local daemon_bin  = resolve_milliways_bin('milliwaysd')

if not path_env:find('/usr/bin', 1, true) then
  path_env = '/usr/bin:' .. path_env
end
if app_bin and app_bin ~= '' and not path_env:find(app_bin, 1, true) then
  path_env = app_bin .. ':' .. path_env
end
if not path_env:find(local_bin, 1, true) then
  path_env = path_env .. ':' .. local_bin
end
config.set_environment_variables = {
  PATH = path_env,
  TERM = 'xterm-256color',
  COLORTERM = 'truecolor',
  TERM_PROGRAM = 'WezTerm',
  MILLIWAYS_HIGHLIGHT_STYLE = os.getenv('MILLIWAYS_HIGHLIGHT_STYLE') or 'catppuccin-mocha',
  MILLIWAYS_FORCE_COLOR = os.getenv('MILLIWAYS_FORCE_COLOR') or '1',
}

-- Every new tab/pane runs `milliways`, which (since v0.6.0) drops directly
-- into the chat REPL when launched inside milliways-term: the launcher
-- detects WEZTERM_EXECUTABLE and routes to runChat() instead of
-- modeCockpit (which would have recursively re-execed milliways-term).
--
-- For a plain shell escape, use Leader + z (binds `os.getenv('SHELL')`).
config.default_prog = { mw_bin }

-- ── State paths ──────────────────────────────────────────────────────────────

local home      = os.getenv('HOME') or ''
local xdg       = os.getenv('XDG_RUNTIME_DIR') or ''
local state_dir = (xdg ~= '' and xdg .. '/milliways') or (home .. '/.local/state/milliways')
local observe_cur = state_dir .. '/observe.cur'

-- ── Header status ────────────────────────────────────────────────────────────

local abbrs = {
  claude  = 'C',
  codex   = 'X',
  copilot = 'Cp',
  minimax = 'M',
  gemini  = 'G',
  ['local'] = 'L',
  pool    = 'P',
}

-- Per-client accent colors. Each entry: { accent, cursor, tab_bg, tab_fg, bar_bg }
-- accent  = status bar highlight color for the active client indicator
-- cursor  = terminal cursor color
-- tab_bg  = active tab background
-- tab_fg  = active tab foreground
-- bar_bg  = status bar background when this client is active
local client_themes = {
  claude  = { accent='#f4f1e8', cursor='#f4f1e8', tab_bg='#24231f', tab_fg='#fffaf0', bar_bg='#151410' },
  codex   = { accent='#ffb454', cursor='#ffb454', tab_bg='#2b1a00', tab_fg='#ffd08a', bar_bg='#180f00' },
  copilot = { accent='#5f8cff', cursor='#5f8cff', tab_bg='#071633', tab_fg='#a9c2ff', bar_bg='#040b1a' },
  minimax = { accent='#af87d7', cursor='#af87d7', tab_bg='#21132f', tab_fg='#d7b8ff', bar_bg='#130a1c' },
  gemini  = { accent='#ff8700', cursor='#ff8700', tab_bg='#2b1300', tab_fg='#ffbd66', bar_bg='#170a00' },
  ['local'] = { accent='#d70000', cursor='#d70000', tab_bg='#2a0000', tab_fg='#ff8a8a', bar_bg='#150000' },
  pool    = { accent='#87d7ff', cursor='#87d7ff', tab_bg='#061c2a', tab_fg='#b8e7ff', bar_bg='#031018' },
}
local default_theme = { accent='#4db51f', cursor='#4db51f', tab_bg='#1d2021', tab_fg='#ebdbb2', bar_bg='#1d2021' }

local last_client = ''
local last_security_banner_key = ''

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

local function as_number(v)
  if type(v) == 'number' then return v end
  if type(v) == 'string' then return tonumber(v) or 0 end
  return 0
end

local function format_count(n)
  n = as_number(n)
  if n < 1000 then return tostring(math.floor(n)) end
  if n < 1000000 then return string.format('%.1fk', n / 1000) end
  return string.format('%.1fm', n / 1000000)
end

local function format_cost(n)
  n = as_number(n)
  if n <= 0 then return '$0.00' end
  if n < 0.01 then return string.format('$%.4f', n) end
  if n < 10 then return string.format('$%.2f', n) end
  return string.format('$%.1f', n)
end

local function security_badge(sec)
  if type(sec) ~= 'table' then return nil end
  local mode = sec.mode or 'warn'
  local posture = string.lower(sec.posture or '')
  local warnings = as_number(sec.warnings or sec.warning_count)
  local blocks = as_number(sec.blocks or sec.block_count)
  if blocks > 0 then
    posture = 'block'
  elseif (sec.startup_scan_required == true or sec.startup_scan_stale == true) and posture == 'ok' then
    posture = 'warn'
  elseif warnings > 0 and posture == '' then
    posture = 'warn'
  elseif posture == '' then
    posture = 'ok'
  end
  if sec.installed == false and posture == 'ok' then
    posture = 'warn'
  end

  if posture == 'block' then
    return {
      label = 'SEC BLOCK ' .. tostring(math.floor(blocks)),
      color = '#fb4934',
      key = 'block:' .. tostring(math.floor(blocks)) .. ':' .. tostring(math.floor(warnings)),
      banner = blocks > 0,
      message = 'SEC BLOCK ' .. tostring(math.floor(blocks)) .. ' · mode ' .. mode,
    }
  end
  if posture == 'warn' then
    local suffix = warnings > 0 and (' ' .. tostring(math.floor(warnings))) or ''
    return {
      label = 'SEC WARN' .. suffix,
      color = '#fabd2f',
      key = 'warn:' .. tostring(math.floor(warnings)) .. ':' .. tostring(math.floor(blocks)),
      banner = warnings > 0,
      message = 'SEC WARN' .. suffix .. ' · mode ' .. mode,
    }
  end
  return {
    label = 'SEC OK',
    color = '#8ec07c',
    key = 'ok',
    banner = false,
    message = 'SEC OK',
  }
end

local function pane_path(pane)
  local uri = pane and pane.current_working_dir
  if uri and uri ~= '' then
    local path = uri:gsub('^file://', '')
    path = path:gsub('%%20', ' ')
    return abbrev_path(path)
  end
  local data = read_observe()
  if data and data.p and data.p ~= '' then
    return abbrev_path(data.p)
  end
  return '~'
end

wezterm.on('update-status', function(window, _pane)
  local data = read_observe()
  if not data then
    window:set_left_status(wezterm.format({
      { Foreground = { AnsiColor = 'Grey' } },
      { Text = ' milliways: daemon not running ' },
    }))
    window:set_right_status('')
    return
  end

  local ver      = data.v or '?'
  local cwd      = abbrev_path(data.p or '')
  local current  = data.c or ''
  local agents   = data.a or {}
  local woke_ago = data.woke_ago  -- seconds since wake, or nil
  local tokens_in = as_number(data.tin or data.tokens_in)
  local tokens_out = as_number(data.tout or data.tokens_out)
  local cost = as_number(data.cost or data.cost_usd)
  local errors = as_number(data.errors or data.errors_5m)
  local sec = security_badge(data.sec or data.security)

  if sec and sec.banner and sec.key ~= last_security_banner_key then
    last_security_banner_key = sec.key
    window:toast_notification('MilliWays security', sec.message, nil, 6000)
  end

  -- Apply per-client color theme when client changes.
  local theme = client_themes[current] or default_theme
  if current ~= last_client then
    last_client = current
    window:set_config_overrides({
      colors = {
        cursor_bg    = theme.cursor,
        cursor_fg    = '#000000',
        cursor_border = theme.cursor,
        tab_bar = {
          active_tab = {
            bg_color  = theme.tab_bg,
            fg_color  = theme.tab_fg,
            intensity = 'Bold',
          },
          inactive_tab = {
            bg_color = '#1d2021',
            fg_color = '#7c6f64',
          },
        },
      },
    })
  end

  local cells = {
    { Background = { Color = theme.bar_bg } },
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
    table.insert(cells, { Foreground = { Color = theme.accent } })
    table.insert(cells, { Text = ' ●' .. current .. ' ' })
  else
    table.insert(cells, { Foreground = { Color = '#504945' } })
    table.insert(cells, { Text = ' ○— ' })
  end

  if tokens_in > 0 or tokens_out > 0 or cost > 0 then
    table.insert(cells, { Foreground = { Color = '#504945' } })
    table.insert(cells, { Text = '│' })
    table.insert(cells, { Foreground = { Color = '#8ec07c' } })
    table.insert(cells, { Text = ' ' .. format_count(tokens_in) .. '↑/' .. format_count(tokens_out) .. '↓ tok ' })
    table.insert(cells, { Foreground = { Color = theme.accent } })
    table.insert(cells, { Text = format_cost(cost) .. ' ' })
  end

  if errors > 0 then
    table.insert(cells, { Foreground = { Color = '#504945' } })
    table.insert(cells, { Text = '│' })
    table.insert(cells, { Foreground = { Color = '#fb4934' } })
    table.insert(cells, { Text = ' err:' .. tostring(math.floor(errors)) .. ' ' })
  end

  if sec then
    table.insert(cells, { Foreground = { Color = '#504945' } })
    table.insert(cells, { Text = '│' })
    table.insert(cells, { Foreground = { Color = sec.color } })
    table.insert(cells, { Text = ' ' .. sec.label .. ' ' })
  end

  table.insert(cells, { Foreground = { Color = '#504945' } })
  table.insert(cells, { Text = '│' })

  for i, agent in ipairs(agents) do
    local abbr = abbrs[agent] or agent:sub(1, 1):upper()
    local fg   = (agent == current) and theme.accent or '#7c6f64'
    table.insert(cells, { Foreground = { Color = fg } })
    table.insert(cells, { Text = ' ' .. i .. ':' .. abbr })
  end
  table.insert(cells, { Text = ' ' })

  window:set_left_status(wezterm.format(cells))
  window:set_right_status('')
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
      command = { args = { mwctl_bin, 'open', '--agent', 'claude' } },
    },
  },
  -- Leader + z  →  plain shell tab (escape hatch)
  {
    key = 'z', mods = 'LEADER',
    action = act.SpawnCommandInNewTab { args = { os.getenv('SHELL') or 'zsh' } },
  },
  -- Leader + 1..7  →  switch active runner
  {
    key = '1', mods = 'LEADER',
    action = act.SpawnCommandInNewTab { args = { mwctl_bin, 'open', '--agent', 'claude' } },
  },
  {
    key = '2', mods = 'LEADER',
    action = act.SpawnCommandInNewTab { args = { mwctl_bin, 'open', '--agent', 'codex' } },
  },
  {
    key = '3', mods = 'LEADER',
    action = act.SpawnCommandInNewTab { args = { mwctl_bin, 'open', '--agent', 'copilot' } },
  },
  {
    key = '4', mods = 'LEADER',
    action = act.SpawnCommandInNewTab { args = { mwctl_bin, 'open', '--agent', 'minimax' } },
  },
  {
    key = '5', mods = 'LEADER',
    action = act.SpawnCommandInNewTab { args = { mwctl_bin, 'open', '--agent', 'gemini' } },
  },
  {
    key = '6', mods = 'LEADER',
    action = act.SpawnCommandInNewTab { args = { mwctl_bin, 'open', '--agent', 'local' } },
  },
  {
    key = '7', mods = 'LEADER',
    action = act.SpawnCommandInNewTab { args = { mwctl_bin, 'open', '--agent', 'pool' } },
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
                act.SpawnCommandInNewTab { args = { mwctl_bin, 'open', '--agent', cur } },
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
    action = act.SpawnCommandInNewTab { args = { mwctl_bin, 'context', '--all' } },
  },
  -- Leader + w  →  observability render overlay
  {
    key = 'w', mods = 'LEADER',
    action = act.SpawnCommandInNewTab { args = { mwctl_bin, 'observe-render' } },
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
  -- Secure MilliWays
  { label = 'security status           show Secure MilliWays posture',             id = 'security status' },
  { label = 'security startup-scan      run startup posture scan',                  id = 'security startup-scan ' },
  { label = 'security cra              show CRA readiness',                        id = 'security cra' },
  { label = 'security cra-scaffold     create CRA evidence placeholders',          id = 'security cra-scaffold ' },
  { label = 'security client …         run client profile checks',                 id = 'security client ' },
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
  local args = { mwctl_bin }
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

-- ── Window/tab title: keep app chrome branded, independent of side panes ─────
local function milliways_title(pane)
  return 'MilliWays:' .. pane_path(pane)
end

wezterm.on('format-window-title', function(_tab, pane)
  return milliways_title(pane)
end)

wezterm.on('format-tab-title', function(tab, _tabs, _panes, _cfg, _hover, max_width)
  local title = milliways_title(tab.active_pane)
  if #title > max_width - 2 then
    title = wezterm.truncate_right(title, max_width - 3) .. '…'
  end
  title = ' ' .. title .. ' '
  local is_active = tab.is_active
  return {
    { Background = { Color = is_active and '#504945' or '#1d2021' } },
    { Foreground = { Color = is_active and '#ebdbb2' or '#a89984' } },
    { Text = title },
  }
end)

-- ── Auto-start milliwaysd when wezterm opens ─────────────────────────────────
-- Spawns the daemon once; subsequent windows reuse the existing socket.
-- Also maximizes the initial window so milliways fills the screen on launch.

wezterm.on('gui-startup', function(cmd)
  local pane_env = {
    PATH = path_env,
    TERM = 'xterm-256color',
    COLORTERM = 'truecolor',
    TERM_PROGRAM = 'WezTerm',
    MILLIWAYS_HIGHLIGHT_STYLE = 'catppuccin-mocha',
    MILLIWAYS_FORCE_COLOR = '1',
  }
  local spawn = cmd or {}
  if not spawn.args then
    spawn.args = { mw_bin }
  end
  spawn.set_environment_variables = spawn.set_environment_variables or {}
  for k, v in pairs(pane_env) do
    spawn.set_environment_variables[k] = v
  end
  spawn.set_environment_variables.MILLIWAYS_NO_DECK = '1'

  local _tab, main_pane, window = mux.spawn_window(spawn)
  if not main_pane then
    return
  end

  window:gui_window():maximize()

  local daemon_sock = state_dir .. '/sock'
  local f = io.open(daemon_sock, 'r')
  if f then
    f:close()
  else
    wezterm.background_child_process({ daemon_bin })
  end
  wezterm.background_child_process({ mwctl_bin, 'observe', '--watch' })

  local main_pane_id = tostring(main_pane:pane_id())
  local deck_pane = main_pane:split {
    direction = 'Left',
    size = 0.25,
    args = { mw_bin, 'attach', '--deck', '--right-pane', main_pane_id },
    set_environment_variables = pane_env,
  }
  if deck_pane then
    deck_pane:split {
      direction = 'Bottom',
      size = 0.25,
      args = { mwctl_bin, 'observe-render' },
      set_environment_variables = pane_env,
    }
  end

  main_pane:activate()
end)

return config

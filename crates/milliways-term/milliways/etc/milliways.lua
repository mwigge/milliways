-- milliways.lua — wezterm configuration helpers for the milliways cockpit:
-- status bar (left-status) and cockpit keybindings.
--
-- One-line user setup from ~/.config/wezterm/wezterm.lua:
--
--   local wezterm   = require 'wezterm'
--   local milliways = require 'milliways'
--   local config    = wezterm.config_builder()
--   milliways.apply(config)   -- status bar + keybindings
--   return config
--
-- Granular API (call one or both as needed):
--
--   milliways.apply_status_bar(config)   -- left-status only
--   milliways.apply_keybindings(config)  -- cockpit hotkeys only
--
-- Status bar:
--   Driven once per second by wezterm's `update-right-status` event,
--   which calls `milliwaysctl status --json` and renders the result as:
--
--     {agent} | turn:{n} | {in}↑/{out}↓ tok | ${cost} | quota: {pct}% | err:{n}
--
--   Field overflow priority (least to most important): error count is
--   elided first, then quota. Agent and tokens are never elided.
--
-- Cockpit keybindings (Cmd is the Super key on Linux/Windows):
--
--   Cmd+Shift+A  — InputSelector listing agents from `milliwaysctl agents`,
--                  spawns a pane in the AgentDomain ("agents") with
--                  MILLIWAYS_AGENT_ID set to the chosen agent.
--   Cmd+Shift+C  — _context pane (active agent's `/context` summary).
--   Cmd+Shift+G  — _context_all pane (aggregated `/context` across agents).
--   Cmd+Shift+O  — _observability pane (cockpit observability view).
--
-- This file is self-contained and only requires `wezterm`, which is
-- always available inside the wezterm config context.

local wezterm = require 'wezterm'

local M = {}

------------------------------------------------------------------------
-- Theme — semantic palette. Per Decision 12 (visual contract) these
-- are the ONLY hex literals in this file. Everywhere else uses
-- `M.theme.*` references so users can recolour the cockpit by
-- mutating this table before calling `apply_status_bar`.
------------------------------------------------------------------------
M.theme = {
  ok     = '#85b94e',
  warn   = '#e0af68',
  err    = '#f7768e',
  accent = '#7aa2f7',
  dim    = '#5c6370',
}

------------------------------------------------------------------------
-- Per-client color themes applied via set_config_overrides when the
-- active agent changes. Keys match the agent id string returned by
-- `milliwaysctl status --json` in the `active_agent` field.
------------------------------------------------------------------------
M.client_themes = {
  claude  = { accent='#f4f1e8', cursor='#f4f1e8', tab_bg='#24231f', tab_fg='#fffaf0', bar_bg='#151410' },
  codex   = { accent='#ffb454', cursor='#ffb454', tab_bg='#2b1a00', tab_fg='#ffd08a', bar_bg='#180f00' },
  copilot = { accent='#5f8cff', cursor='#5f8cff', tab_bg='#071633', tab_fg='#a9c2ff', bar_bg='#040b1a' },
  minimax = { accent='#af87d7', cursor='#af87d7', tab_bg='#21132f', tab_fg='#d7b8ff', bar_bg='#130a1c' },
  gemini  = { accent='#ff8700', cursor='#ff8700', tab_bg='#2b1300', tab_fg='#ffbd66', bar_bg='#170a00' },
  ['local'] = { accent='#d70000', cursor='#d70000', tab_bg='#2a0000', tab_fg='#ff8a8a', bar_bg='#150000' },
  pool    = { accent='#87d7ff', cursor='#87d7ff', tab_bg='#061c2a', tab_fg='#b8e7ff', bar_bg='#031018' },
}
M.default_client_theme = { accent='#7aa2f7', cursor='#7aa2f7', tab_bg='#1d2021', tab_fg='#ebdbb2', bar_bg='#1d2021' }

M.hyperlink_rules = {
  { regex = '\\b(https?://[\\w\\-@:%.+~#=/?&]+)', highlight = 1, format = '$1' },
  { regex = '\\b(?:issue|issues|pr|PR)[: ]#?(\\d+)\\b', highlight = 1,
    format = 'https://github.com/mwigge/milliways/issues/$1' },
  { regex = '(~/[\\w\\-./]+|/[\\w\\-./]+):(\\d+)\\b', highlight = 1, format = 'file://$1:$2' },
  { regex = '(~/[\\w\\-./]+|/[\\w\\-./]+\\.[a-zA-Z0-9]+)', highlight = 1, format = 'file://$1' },
  { regex = '\\b([a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+[#@][a-zA-Z0-9]+)\\b',
    highlight = 1, format = 'https://github.com/$1' },
  { regex = '\\b(github\\.com/[\\w\\-./]+)\\b', highlight = 1, format = 'https://$1' },
  { regex = '#(\\d+)\\b', highlight = 0,
    format = 'https://github.com/mwigge/milliways/issues/$1' },
}

-- Tracks the last seen active agent to avoid redundant override calls.
local last_client = ''

------------------------------------------------------------------------
-- Internal cache for the last successful status fetch. The
-- update-right-status hook is best-effort: if the daemon is slow
-- (>200ms) we render the previous status with a `…` suffix instead
-- of blocking the UI.
------------------------------------------------------------------------
local cache = {
  status     = nil,  -- last decoded JSON table
  fetched_at = 0,    -- unix seconds when last successfully fetched
  stale      = false,-- true if last attempt missed the deadline
}

-- Soft deadline (seconds) for the synchronous milliwaysctl call.
-- wezterm.run_child_process is synchronous; we time it and treat any
-- call beyond this threshold as stale.
local FETCH_DEADLINE_S = 0.200

------------------------------------------------------------------------
-- Default formatter. Users override by setting
-- `milliways.status_format = function(s) ... end` before calling
-- apply_status_bar. The function receives the decoded JSON table and
-- returns a list of `{ {Foreground=...}, {Text=...}, ... }` segments
-- consumable by `wezterm.format`.
------------------------------------------------------------------------

local function default_agent(status)
  if status and status.active_agent and status.active_agent ~= '' then
    return tostring(status.active_agent)
  end
  return '-'
end

local function default_cost(status)
  local c = (status and status.cost_usd) or 0
  return string.format('$%.2f', c)
end

local function default_quota_pct(status)
  if not status then return nil end
  -- Accept either a precomputed percentage or {used,limit}.
  if status.quota_pct ~= nil then return tonumber(status.quota_pct) end
  local q = status.quota
  if type(q) == 'table' and q.limit and q.limit > 0 then
    return math.floor(((q.limit - (q.used or 0)) / q.limit) * 100)
  end
  return nil
end

local function default_security_label(status)
  if not status then return nil, nil end
  local sec = status.security or status.sec or status
  if type(sec) ~= 'table' then return nil, nil end
  local mode = sec.mode or 'warn'
  local posture = string.lower(sec.posture or sec.state or '')
  local warnings = tonumber(sec.warnings or sec.warning_count or sec.warn_count or 0) or 0
  local blocks = tonumber(sec.blocks or sec.block_count or sec.blocked_count or 0) or 0
  local startup_stale = sec.startup_scan_stale == true
  local startup_required = sec.startup_scan_required == true
  if blocks > 0 then
    posture = 'block'
  elseif (startup_stale or startup_required) and (posture == '' or posture == 'ok') then
    posture = 'warn'
  elseif warnings > 0 and posture == '' then
    posture = 'warn'
  elseif posture == '' then
    posture = 'ok'
  end
  if posture == 'block' then
    return string.format('SEC BLOCK %d', blocks), M.theme.err
  end
  if posture == 'warn' then
    local suffix = warnings > 0 and string.format(' %d', warnings) or ''
    if startup_stale then suffix = suffix .. ' stale' end
    return 'SEC WARN' .. suffix .. ' ' .. mode, M.theme.warn
  end
  return 'SEC OK ' .. mode, M.theme.ok
end

local function tokens_pair(status)
  local tin  = (status and status.tokens_in)  or 0
  local tout = (status and status.tokens_out) or 0
  return tin, tout
end

-- Available width estimate (best-effort). wezterm does not expose the
-- precise column budget for the right status, so we use a generous
-- default and clip if the joined string exceeds it.
local DEFAULT_BUDGET = 80

local function elide(parts, budget)
  -- parts is an array of {priority=int, text=string, fg=string}
  -- Higher priority means "elide first". Agent (0) and tokens (0) MUST
  -- never elide. Quota is 1, error count is 2.
  local function rendered_len(active)
    local n = 0
    for _, p in ipairs(active) do
      if not p.elided then
        n = n + #p.text + 3 -- +3 for separator " | "
      end
    end
    return n
  end
  -- Mark elision in priority order until under budget.
  for prio = 2, 1, -1 do
    if rendered_len(parts) <= budget then break end
    for _, p in ipairs(parts) do
      if p.priority == prio then p.elided = true end
    end
  end
  return parts
end

function M.status_format(status, opts)
  opts = opts or {}
  local theme = opts.theme or M.theme
  local stale = opts.stale or false
  local budget = opts.budget or DEFAULT_BUDGET

  local agent = default_agent(status)
  local turns = (status and status.turns) or (status and status.turn) or 0
  local tin, tout = tokens_pair(status)
  local cost = default_cost(status)
  local quota_pct = default_quota_pct(status)
  local errs = (status and status.errors_5m) or (status and status.error_count) or 0

  local parts = {
    -- agent and tokens are never elided (priority 0)
    { priority = 0, fg = theme.accent, text = agent },
    { priority = 0, fg = theme.dim,    text = string.format('turn:%d', turns) },
    { priority = 0, fg = theme.ok,     text = string.format('%d↑/%d↓ tok', tin, tout) },
    -- cost stays visible (priority 0 — informative, not optional)
    { priority = 0, fg = theme.accent, text = cost },
  }
  if quota_pct ~= nil then
    local fg = theme.ok
    if quota_pct < 25 then fg = theme.err
    elseif quota_pct < 50 then fg = theme.warn end
    table.insert(parts, { priority = 1, fg = fg,
      text = string.format('quota: %d%%', quota_pct) })
  end
  if errs and errs > 0 then
    table.insert(parts, { priority = 2, fg = theme.err,
      text = string.format('err:%d', errs) })
  end
  local sec_text, sec_fg = default_security_label(status)
  if sec_text then
    table.insert(parts, { priority = 1, fg = sec_fg, text = sec_text })
  end

  parts = elide(parts, budget)

  local segments = {}
  local first = true
  for _, p in ipairs(parts) do
    if not p.elided then
      if not first then
        table.insert(segments, { Foreground = { Color = theme.dim } })
        table.insert(segments, { Text = ' | ' })
      end
      table.insert(segments, { Foreground = { Color = p.fg } })
      table.insert(segments, { Text = p.text })
      first = false
    end
  end
  if stale then
    table.insert(segments, { Foreground = { Color = theme.warn } })
    table.insert(segments, { Text = ' …' })
  end
  return segments
end

------------------------------------------------------------------------
-- Fetch status. Calls `milliwaysctl status --json` with a 200ms soft
-- deadline. wezterm exposes only the synchronous run_child_process,
-- so the deadline is enforced by measuring elapsed time and falling
-- back to the cached value if the call overran. We also probe for a
-- non-blocking variant (wezterm.background_child_process) in case a
-- newer wezterm exposes it.
------------------------------------------------------------------------
local function fetch_status_now()
  local started = os.clock()
  local ok, success, stdout, _stderr = pcall(function()
    return wezterm.run_child_process({ 'milliwaysctl', 'status', '--json' })
  end)
  local elapsed = os.clock() - started
  if not ok or not success or not stdout or stdout == '' then
    return nil, elapsed, 'fetch failed'
  end
  local decoded_ok, decoded = pcall(wezterm.json_parse, stdout)
  if not decoded_ok or type(decoded) ~= 'table' then
    return nil, elapsed, 'json parse failed'
  end
  return decoded, elapsed, nil
end

local function refresh_cache()
  local status, elapsed, _err = fetch_status_now()
  if status ~= nil and elapsed <= FETCH_DEADLINE_S then
    cache.status     = status
    cache.fetched_at = os.time()
    cache.stale      = false
    return status, false
  end
  -- Either the call overran the deadline or it failed outright. Mark
  -- the cached value stale so the formatter renders the `…` suffix.
  cache.stale = true
  return cache.status, true
end

------------------------------------------------------------------------
-- apply_status_bar(config) — wire the update-status hook into
-- the user's wezterm config. Safe to call multiple times.
------------------------------------------------------------------------
function M.apply_status_bar(config)
  config.status_update_interval = config.status_update_interval or 500

  wezterm.on('update-status', function(window, _pane)
    local status, stale = refresh_cache()

    -- Apply per-client color theme when the active agent changes.
    local agent = (status and status.active_agent) or ''
    if type(agent) ~= 'string' then agent = '' end
    if agent ~= last_client then
      last_client = agent
      local ct = M.client_themes[agent] or M.default_client_theme
      window:set_config_overrides({
        colors = {
          cursor_bg     = ct.cursor,
          cursor_fg     = '#000000',
          cursor_border = ct.cursor,
          tab_bar = {
            active_tab = {
              bg_color  = ct.tab_bg,
              fg_color  = ct.tab_fg,
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

    -- User-provided formatter can completely replace the default. If
    -- it errors, fall back to the default per the spec's "Custom
    -- format" scenario.
    local formatter = M.status_format
    local segments
    local ok, result = pcall(formatter, status, { theme = M.theme, stale = stale })
    if ok and type(result) == 'table' then
      segments = result
    else
      segments = M.status_format(status, { theme = M.theme, stale = stale })
    end

    window:set_left_status(wezterm.format(segments))
    window:set_right_status('')
  end)
end

------------------------------------------------------------------------
-- Cockpit keybindings.
--
-- All four bindings spawn a pane in the AgentDomain (`{ DomainName =
-- "agents" }`). The AgentDomain reads `MILLIWAYS_AGENT_ID` from the
-- spawn env to decide which agent the pane talks to. We pass `args =
-- {"true"}` as a no-op program — AgentDomain replaces the spawn with a
-- `milliwaysctl bridge` invocation under the hood, so the args here are
-- only there to satisfy SpawnCommand's "must have args or default
-- prog" rule on platforms where the default prog is unavailable.
--
-- The Cmd+Shift+A picker fetches the list dynamically from
-- `milliwaysctl agents`. If the daemon is unreachable we fall back to
-- the four built-in runner ids plus the `_echo` demo agent so the user
-- still gets a usable picker.
------------------------------------------------------------------------

-- Built-in fallback list when `milliwaysctl agents` cannot be reached.
-- These mirror the runner ids registered in internal/daemon/agents.go.
local FALLBACK_AGENTS = { 'claude', 'codex', 'copilot', 'minimax', 'gemini', 'local', 'pool' }

-- fetch_agent_choices returns a list of `{ id = <agent_id>, label =
-- <display> }` tables suitable for InputSelector.choices. Best-effort:
-- a daemon failure falls back to FALLBACK_AGENTS so the picker still
-- works during first-run / daemon-down scenarios.
local function fetch_agent_choices()
  local choices = {}
  local ok, success, stdout, _stderr = pcall(function()
    return wezterm.run_child_process({ 'milliwaysctl', 'agents' })
  end)
  if ok and success and stdout and stdout ~= '' then
    local decoded_ok, decoded = pcall(wezterm.json_parse, stdout)
    if decoded_ok and type(decoded) == 'table' then
      for _, entry in ipairs(decoded) do
        if type(entry) == 'table' and entry.id then
          local label = tostring(entry.id)
          if entry.available == false then
            label = label .. ' (unavailable)'
          elseif entry.model and entry.model ~= '' then
            label = label .. ' — ' .. tostring(entry.model)
          end
          table.insert(choices, { id = tostring(entry.id), label = label })
        elseif type(entry) == 'string' then
          table.insert(choices, { id = entry, label = entry })
        end
      end
    end
  end
  if #choices == 0 then
    for _, id in ipairs(FALLBACK_AGENTS) do
      table.insert(choices, { id = id, label = id })
    end
  end
  return choices
end

-- spawn_agent_pane builds the SpawnCommandInNewTab action that opens a
-- new pane in the AgentDomain for `agent_id`.
local function spawn_agent_pane(agent_id)
  return wezterm.action.SpawnCommandInNewTab {
    domain = { DomainName = 'agents' },
    args = { 'true' },
    set_environment_variables = {
      MILLIWAYS_AGENT_ID = agent_id,
    },
  }
end

------------------------------------------------------------------------
-- apply_keybindings(config) — wire the four cockpit hotkeys into the
-- user's wezterm config. Existing `config.keys` entries are preserved;
-- ours are appended.
------------------------------------------------------------------------
function M.apply_keybindings(config)
  config.keys = config.keys or {}

  -- Cmd+Shift+A — agent picker (InputSelector → SpawnCommandInNewTab).
  table.insert(config.keys, {
    key = 'A',
    mods = 'CMD|SHIFT',
    action = wezterm.action_callback(function(window, pane)
      window:perform_action(
        wezterm.action.InputSelector {
          title = 'milliways: pick an agent',
          fuzzy = true,
          fuzzy_description = 'Filter agents: ',
          choices = fetch_agent_choices(),
          action = wezterm.action_callback(function(inner_window, inner_pane, id, _label)
            if not id then
              return  -- cancelled
            end
            inner_window:perform_action(spawn_agent_pane(id), inner_pane)
          end),
        },
        pane
      )
    end),
  })

  -- Cmd+Shift+C — `/context` pane for the active agent.
  table.insert(config.keys, {
    key = 'C',
    mods = 'CMD|SHIFT',
    action = spawn_agent_pane('_context'),
  })

  -- Cmd+Shift+G — aggregated `/context` pane.
  table.insert(config.keys, {
    key = 'G',
    mods = 'CMD|SHIFT',
    action = spawn_agent_pane('_context_all'),
  })

  -- Cmd+Shift+O — observability cockpit pane.
  table.insert(config.keys, {
    key = 'O',
    mods = 'CMD|SHIFT',
    action = spawn_agent_pane('_observability'),
  })
end

------------------------------------------------------------------------
-- apply(config) — single-line setup: wires the status bar and the
-- cockpit keybindings. Equivalent to calling apply_status_bar followed
-- by apply_keybindings.
------------------------------------------------------------------------
function M.apply(config)
  config.window_padding = config.window_padding or { left = 6, right = 6, top = 4, bottom = 4 }
  config.check_for_updates = false
  config.bypass_mouse_reporting_modifiers = config.bypass_mouse_reporting_modifiers or 'SHIFT'
  config.set_environment_variables = config.set_environment_variables or {}
  config.set_environment_variables.COLORTERM = config.set_environment_variables.COLORTERM or 'truecolor'
  config.set_environment_variables.TERM = config.set_environment_variables.TERM or 'xterm-256color'
  config.set_environment_variables.TERM_PROGRAM = config.set_environment_variables.TERM_PROGRAM or 'WezTerm'
  config.set_environment_variables.MILLIWAYS_HIGHLIGHT_STYLE = config.set_environment_variables.MILLIWAYS_HIGHLIGHT_STYLE or 'catppuccin-mocha'
  config.set_environment_variables.MILLIWAYS_FORCE_COLOR = config.set_environment_variables.MILLIWAYS_FORCE_COLOR or '1'
  config.hyperlink_rules = config.hyperlink_rules or M.hyperlink_rules
  M.apply_status_bar(config)
  M.apply_keybindings(config)
end

------------------------------------------------------------------------
-- Test/inspection hooks (kept on M for unit-test friendliness).
------------------------------------------------------------------------
M._cache             = cache
M._FETCH_DEADLINE_S  = FETCH_DEADLINE_S
M._refresh_cache     = refresh_cache
M._FALLBACK_AGENTS   = FALLBACK_AGENTS
M._fetch_agent_choices = fetch_agent_choices
M._spawn_agent_pane  = spawn_agent_pane

return M

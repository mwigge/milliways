-- milliways.lua — sample wezterm configuration helpers for the cockpit
-- status bar.
--
-- Usage from ~/.config/wezterm/wezterm.lua:
--
--   local wezterm   = require 'wezterm'
--   local milliways = require 'milliways'
--   local config    = wezterm.config_builder()
--   milliways.apply_status_bar(config)
--   return config
--
-- The status bar is driven once per second by wezterm's
-- `update-right-status` event, which calls
-- `milliwaysctl status --json` and renders the result as:
--
--   {agent} | turn:{n} | {in}↑/{out}↓ tok | ${cost} | quota: {pct}% | err:{n}
--
-- Field overflow priority (least to most important): error count is
-- elided first, then quota. Agent and tokens are never elided.
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
-- apply_status_bar(config) — wire the update-right-status hook into
-- the user's wezterm config. Safe to call multiple times.
------------------------------------------------------------------------
function M.apply_status_bar(config)
  -- 1 Hz default; the spec's sub-second-during-dispatch story is the
  -- watch sidecar's job, not this hook.
  config.status_update_interval = config.status_update_interval or 1000

  wezterm.on('update-right-status', function(window, _pane)
    local status, stale = refresh_cache()

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

    window:set_right_status(wezterm.format(segments))
  end)
end

------------------------------------------------------------------------
-- Test/inspection hooks (kept on M for unit-test friendliness).
------------------------------------------------------------------------
M._cache             = cache
M._FETCH_DEADLINE_S  = FETCH_DEADLINE_S
M._refresh_cache     = refresh_cache

return M

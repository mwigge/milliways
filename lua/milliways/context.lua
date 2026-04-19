-- context.lua — Editor context collectors for milliways.nvim L2.
-- All collectors return nil on absence rather than erroring.
-- Each collector has a configurable timeout (default 15ms).

local M = {}

-- Per-collector timeout in milliseconds.
-- Override with M.configure({ timeout_ms = N }).
M.timeout_ms = 15

function M.configure(opts)
  if opts.timeout_ms then
    M.timeout_ms = opts.timeout_ms
  end
end

-- Collect buffer state: path, filetype, modified, lines count, visible range.
-- Returns nil if no buffer is available.
function M.collect_buffer()
  local bufnr = vim.api.nvim_get_current_buf()
  if not bufnr or not vim.api.nvim_buf_is_valid(bufnr) then
    return nil
  end

  local abs_path = vim.api.nvim_buf_get_name(bufnr)
  local filetype = vim.bo[bufnr].filetype or ""
  local modified = vim.bo[bufnr].modified
  local lines_count = vim.api.nvim_buf_line_count(bufnr)

  -- Visible range: row1..row2 (1-indexed, inclusive)
  local win_id = vim.fn.win_getid()
  local info = vim.fn.getwininfo(win_id)[1]
  local top_line = info.topline or 1
  local bot_line = info.botline or lines_count

  return {
    type = "buffer",
    path = abs_path,
    filetype = filetype,
    modified = modified,
    total_lines = lines_count,
    visible_start = top_line,
    visible_end = bot_line,
  }
end

-- Collect cursor state: line, column, treesitter scope.
-- Returns nil if not available.
function M.collect_cursor()
  local ok, scope = pcall(function()
    local pos = vim.fn.getcurpos()
    local row = pos[2] -- 0-indexed
    local col = pos[3] -- 0-indexed (byte offset)

    -- Try to get treesitter scope
    local scope_name = ""
    if vim.treesitter.highlighter then
      local buf = vim.api.nvim_get_current_buf()
      local lang = vim.treesitter.language.get_lang(vim.bo.filetype)
      if lang then
        local parser = vim.treesitter.get_parser(buf, lang)
        if parser then
          local tree = parser:parse()
          if tree and tree[1] then
            local root = tree[1]:root()
            -- Find smallest node containing the cursor position
            local node = root:named_descendant_for_range(row, col, row, col)
            if node then
              scope_name = node:type()
            end
          end
        end
      end
    end

    return {
      line = row + 1,  -- convert to 1-indexed
      column = col + 1, -- convert to 1-indexed
      scope = scope_name,
    }
  end)

  if ok and scope then
    return {
      type = "cursor",
      line = scope.line,
      column = scope.column,
      scope = scope.scope,
    }
  end

  -- Fallback without treesitter
  local pos = vim.fn.getcurpos()
  return {
    type = "cursor",
    line = pos[2] + 1,
    column = pos[3] + 1,
    scope = "",
  }
end

-- Collect visual selection if currently in visual mode.
-- Returns nil if not in visual mode or no selection.
function M.collect_selection()
  local mode = vim.fn.mode()
  if not (mode:match("v") or mode == "\22") then -- 'v', 'V', '^V' (Ctrl-V)
    return nil
  end

  local ok, result = pcall(function()
    local start_pos = vim.fn.getpos("'<")
    local end_pos = vim.fn.getpos("'>")
    local start_line = start_pos[2] -- 1-indexed
    local end_line = end_pos[2]     -- 1-indexed
    local start_col = start_pos[3]  -- 1-indexed (byte)
    local end_col = end_pos[3]      -- 1-indexed (byte)

    local lines = vim.api.nvim_buf_get_lines(0, start_line - 1, end_line, false)

    -- Adjust first and last lines for column bounds
    if #lines > 0 then
      lines[#lines] = lines[#lines]:sub(1, end_col - (#lines == 1 and start_col or 1))
      lines[1] = lines[1]:sub(start_col)
    end

    return {
      type = "selection",
      start_line = start_line,
      end_line = end_line,
      text = table.concat(lines, "\n"),
    }
  end)

  if ok and result then
    return result
  end
  return nil
end

-- Collect git state: branch, dirty, files changed, ahead/behind.
-- Returns nil if not in a git repo.
function M.collect_git()
  local ok, result = pcall(function()
    local branch = vim.fn.system({ "git", "rev-parse", "--abbrev-ref", "HEAD" })
    if vim.v.shell_error ~= 0 then return nil end
    branch = vim.trim(branch)

    local status_out = vim.fn.system({ "git", "status", "--porcelain" })
    local dirty = vim.trim(status_out) ~= ""

    local files_changed = 0
    for _ in status_out:gmatch("[^\n]+") do
      files_changed = files_changed + 1
    end

    local revparse_out = vim.fn.system({ "git", "rev-parse", "@{upstream}" })
    local upstream = vim.trim(revparse_out)
    local ahead = 0
    local behind = 0
    if upstream ~= "" then
      local revlist = vim.fn.system({
        "git", "rev-list", "--left-right", "--count",
        string.format("%s...%s", branch, upstream)
      })
      if vim.v.shell_error == 0 then
        local parts = vim.split(vim.trim(revlist), "%s+")
        ahead = tonumber(parts[1]) or 0
        behind = tonumber(parts[2]) or 0
      end
    end

    return {
      type = "git",
      branch = branch,
      dirty = dirty,
      files_changed = files_changed,
      ahead = ahead,
      behind = behind,
    }
  end)

  if ok and result then
    return result
  end
  return nil
end

-- Collect LSP diagnostics for the current buffer.
-- scope: "visible" (default) or "file"
-- Returns nil if no LSP client is attached.
function M.collect_lsp(scope)
  scope = scope or "visible"

  local ok, result = pcall(function()
    local buf = vim.api.nvim_get_current_buf()
    local clients = vim.lsp.get_active_clients({ bufnr = buf })
    if #clients == 0 then return nil end

    local diagnostics = {}
    local max_severity = 4 -- Error=1, Warning=2, Information=3, Hint=4

    -- Get all diagnostics for the buffer
    local buf_diagnostics = vim.diagnostic.get(buf)

    local info = vim.fn.getwininfo(vim.fn.win_getid())[1]
    local top_line = info.topline or 1
    local bot_line = info.botline or vim.api.nvim_buf_line_count(buf)

    for _, diag in ipairs(buf_diagnostics) do
      local lnum = diag.lnum + 1 -- 0-indexed → 1-indexed
      local in_visible = (scope == "visible") and (lnum >= top_line and lnum <= bot_line)
      local in_range = (scope == "file") or in_visible

      if in_range then
        table.insert(diagnostics, {
          severity = diag.severity,
          message = diag.message,
          lnum = lnum,
          end_lnum = (diag.end_lnum or diag.lnum) + 1,
          code = diag.code or "",
        })
      end
    end

    -- Count by severity
    local errors = 0
    local warnings = 0
    for _, d in ipairs(diagnostics) do
      if d.severity == 1 then errors = errors + 1
      elseif d.severity == 2 then warnings = warnings + 1 end
    end

    return {
      type = "lsp",
      scope = scope,
      total = #diagnostics,
      errors = errors,
      warnings = warnings,
      entries = diagnostics,
    }
  end)

  if ok and result then
    return result
  end
  return nil
end

-- Collect project metadata: root, language, open buffers, recent files.
function M.collect_project()
  local ok, result = pcall(function()
    -- Project root: find .git or go.mod or package.json
    local root = vim.fs.root(vim.api.nvim_buf_get_name(0), {
      ".git", "go.mod", "package.json", "Cargo.toml", "pyproject.toml"
    }) or ""

    -- Primary language from treesitter or filetype
    local primary_lang = vim.bo.filetype or ""

    -- Open buffers (non-hidden, non-special)
    local buffers = {}
    for _, buf in ipairs(vim.api.nvim_list_bufs()) do
      if vim.api.nvim_buf_is_loaded(buf) and not vim.bo[buf].buflisted == false then
        local name = vim.api.nvim_buf_get_name(buf)
        if name and name ~= "" then
          table.insert(buffers, {
            path = name,
            filetype = vim.bo[buf].filetype or "",
          })
        end
      end
    end

    -- Recent files from the buffer list (up to 5)
    local recent = {}
    local listed = vim.fn.argv()
    for i = 1, math.min(#listed, 5) do
      if listed[i] and listed[i] ~= "" then
        table.insert(recent, listed[i])
      end
    end

    return {
      type = "project",
      root = root,
      primary_language = primary_lang,
      open_buffers = buffers,
      recent_files = recent,
    }
  end)

  if ok and result then
    return result
  end
  return nil
end

-- Collect quickfix entries (valid when quickfix window is open).
function M.collect_quickfix()
  local ok, result = pcall(function()
    local qfl = vim.fn.getqflist()
    if #qfl == 0 then return nil end

    local entries = {}
    for _, item in ipairs(qfl) do
      table.insert(entries, {
        lnum = item.lnum or 0,
        col = item.col or 0,
        text = item.text or "",
        type = item.type or "",
      })
    end

    return {
      type = "quickfix",
      total = #entries,
      entries = entries,
    }
  end)

  if ok and result then
    return result
  end
  return nil
end

-- Collect loclist entries (valid when loclist window is open).
function M.collect_loclist()
  local ok, result = pcall(function()
    local winnr = vim.fn.winnr()
    local loclist = vim.fn.getloclist(winnr)
    if #loclist == 0 then return nil end

    local entries = {}
    for _, item in ipairs(loclist) do
      table.insert(entries, {
        lnum = item.lnum or 0,
        col = item.col or 0,
        text = item.text or "",
        type = item.type or "",
      })
    end

    return {
      type = "loclist",
      total = #entries,
      entries = entries,
    }
  end)

  if ok and result then
    return result
  end
  return nil
end

-- Build the full editor context bundle.
-- opts: {
--   include_selection = bool|"auto" (default "auto"),
--   include_quickfix = bool|"auto" (default "auto"),
--   include_loclist = bool|"auto" (default "auto"),
--   budget_kb = max total bytes (default 64),
-- }
-- Returns: { schema_version = "1", collectors = {...}, total_bytes = N }
function M.build(opts)
  opts = opts or {}
  local include_selection = opts.include_selection or "auto"
  local include_quickfix = opts.include_quickfix or "auto"
  local include_loclist = opts.include_loclist or "auto"
  local budget_kb = opts.budget_kb or 64

  local bundle = {
    schema_version = "1",
    collected_at = os.date("!%Y-%m-%dT%H:%M:%SZ"), -- ISO 8601 UTC
    collectors = {},
    total_bytes = 0,
  }

  local total = 0
  local budget_bytes = budget_kb * 1024

  local function try_collect(collector_fn, name)
    if total >= budget_bytes then return end
    local result = collector_fn()
    if result then
      local encoded = vim.json.encode(result)
      if (total + #encoded) <= budget_bytes then
        bundle.collectors[name] = result
        total = total + #encoded
      end
    end
  end

  -- Core collectors — always run
  try_collect(M.collect_buffer, "buffer")
  try_collect(M.collect_cursor, "cursor")
  try_collect(M.collect_project, "project")
  try_collect(M.collect_git, "git")
  try_collect(M.collect_lsp, "lsp")

  -- Conditional collectors
  if include_selection == true or (include_selection == "auto" and M.collect_selection()) then
    try_collect(M.collect_selection, "selection")
  end

  if include_quickfix == true or (include_quickfix == "auto") then
    try_collect(M.collect_quickfix, "quickfix")
  end

  if include_loclist == true or (include_loclist == "auto") then
    try_collect(M.collect_loclist, "loclist")
  end

  bundle.total_bytes = total
  return bundle
end

return M

-- float.lua — Floating window management for milliways.nvim.

local M = {}

-- Module-local window state
local float_buf = nil
local float_win = nil
local user_cursor_moved = false
local suppress_cursor_guard = false

-- Configure float dimensions (fraction of editor).
M.config = {
  width = 0.8,
  height = 0.8,
}

M.recent_dispatches = {}
M.recent_idx = 0

function M.configure(opts)
  if opts.float_width then M.config.width = opts.float_width end
  if opts.float_height then M.config.height = opts.float_height end
end

-- Open a new floating window with optional initial content.
-- content: string or list of strings
-- streaming: if true, use streaming mode (line-by-line append)
function M.open(content, streaming)
  -- Close any existing float
  M.close()
  user_cursor_moved = false

  float_buf = vim.api.nvim_create_buf(false, true) -- scratch buffer
  vim.bo[float_buf].bufhidden = "wipe"
  vim.bo[float_buf].filetype = "markdown"

  local width = math.floor(vim.o.columns * M.config.width)
  local height = math.floor(vim.o.lines * M.config.height)
  local row = math.floor((vim.o.lines - height) / 2)
  local col = math.floor((vim.o.columns - width) / 2)

  float_win = vim.api.nvim_open_win(float_buf, true, {
    relative = "editor",
    width = width,
    height = height,
    row = row,
    col = col,
    style = "minimal",
    border = "rounded",
    title = " Milliways ",
    title_pos = "center",
  })

  -- Configure window options
  vim.wo[float_win].wrap = true
  vim.wo[float_win].linebreak = true
  vim.wo[float_win].cursorline = false

  -- Set initial content
  if content then
    M.set_content(content)
  end

  -- Add buffer-local keybindings
  vim.keymap.set("n", "q", function()
    M.close()
  end, { buffer = float_buf, desc = "Close Milliways window" })

  vim.keymap.set("n", "y", function()
    local lines = vim.api.nvim_buf_get_lines(float_buf, 0, -1, false)
    vim.fn.setreg("+", table.concat(lines, "\n"))
    vim.notify("Copied to clipboard", vim.log.levels.INFO)
  end, { buffer = float_buf, desc = "Yank output to clipboard" })

  vim.keymap.set("n", "<Tab>", function()
    M.cycle_recent()
  end, { buffer = float_buf, desc = "Cycle recent conversations", silent = true })

  local function on_cursor_moved()
    if suppress_cursor_guard then
      return
    end
    user_cursor_moved = true
  end

  vim.api.nvim_create_autocmd("CursorMoved", {
    buffer = float_buf,
    callback = on_cursor_moved,
  })

  local movement_keys = { "j", "k", "<Up>", "<Down>", "<LeftMouse>", "<ScrollWheelUp>", "<ScrollWheelDown>" }
  for _, key in ipairs(movement_keys) do
    vim.keymap.set("n", key, function()
      user_cursor_moved = true
      return key
    end, {
      buffer = float_buf,
      desc = "Milliways cursor movement",
      expr = true,
      silent = true,
      replace_keycodes = false,
    })
  end

  return float_buf, float_win
end

-- Set the entire buffer content.
-- content: string or list of strings
function M.set_content(content)
  if not float_buf or not vim.api.nvim_buf_is_valid(float_buf) then
    return
  end
  vim.schedule(function()
    if type(content) == "string" then
      content = vim.split(content, "\n", { plain = true })
    end
    vim.api.nvim_buf_set_lines(float_buf, 0, -1, false, content)
  end)
end

-- Append lines to the buffer (streaming mode).
-- lines: string or list of strings
function M.append(lines)
  if not float_buf or not vim.api.nvim_buf_is_valid(float_buf) then
    return
  end
  vim.schedule(function()
    if type(lines) == "string" then
      lines = vim.split(lines, "\n", { plain = true })
    end
    -- Append at the end
    vim.api.nvim_buf_set_lines(float_buf, -1, -1, false, lines)
    -- Autoscroll to bottom
    if not user_cursor_moved and float_win and vim.api.nvim_win_is_valid(float_win) then
      suppress_cursor_guard = true
      vim.api.nvim_win_set_cursor(float_win, {
        vim.api.nvim_buf_line_count(float_buf),
        0
      })
      vim.schedule(function()
        suppress_cursor_guard = false
      end)
    end
  end)
end

---Resume automatic scrolling for the active float window.
function M.resume_autoscroll()
  user_cursor_moved = false
  suppress_cursor_guard = false
end

---Track a recent dispatch for cycling previews.
---@param prompt string
---@param kitchen string|nil
function M.add_recent(prompt, kitchen)
  table.insert(M.recent_dispatches, 1, {
    prompt = prompt,
    kitchen = kitchen,
    ts = os.time(),
  })
  if #M.recent_dispatches > 3 then
    table.remove(M.recent_dispatches)
  end
  M.recent_idx = 0
end

---Cycle through recent dispatches and preview them in the float.
function M.cycle_recent()
  if #M.recent_dispatches == 0 then
    vim.notify("No recent conversations", vim.log.levels.INFO)
    return
  end

  M.recent_idx = (M.recent_idx % #M.recent_dispatches) + 1
  local conv = M.recent_dispatches[M.recent_idx]

  vim.schedule(function()
    if not float_buf or not vim.api.nvim_buf_is_valid(float_buf) then
      return
    end

    local preview = {
      "--- Recent: " .. (conv.prompt or "?"),
      "Kitchen: " .. (conv.kitchen or "?"),
      "---",
      "",
    }
    local current = vim.api.nvim_buf_get_lines(float_buf, 0, 4, false)
    if #current >= 3 and vim.startswith(current[1], "--- Recent:") then
      vim.api.nvim_buf_set_lines(float_buf, 0, math.min(4, #current), false, preview)
      return
    end
    vim.api.nvim_buf_set_lines(float_buf, 0, 0, false, preview)
  end)
end

-- Close the floating window if open.
function M.close()
  if float_win and vim.api.nvim_win_is_valid(float_win) then
    pcall(vim.api.nvim_win_close, float_win, true)
    float_win = nil
  end
  if float_buf and vim.api.nvim_buf_is_valid(float_buf) then
    pcall(vim.api.nvim_buf_delete, float_buf, { force = true })
    float_buf = nil
  end
  suppress_cursor_guard = false
end

-- Update title (e.g. when kitchen changes mid-dispatch).
function M.set_title(title)
  if float_win and vim.api.nvim_win_is_valid(float_win) then
    local config = vim.api.nvim_win_get_config(float_win)
    config.title = " " .. title .. " "
    config.title_pos = "center"
    vim.api.nvim_win_set_config(float_win, config)
  end
end

-- Return current buffer and window IDs (nil if closed).
function M.get_state()
  return float_buf, float_win
end

return M

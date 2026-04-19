-- float.lua — Floating window management for milliways.nvim.

local M = {}

-- Module-local window state
local float_buf = nil
local float_win = nil

-- Configure float dimensions (fraction of editor).
M.config = {
  width = 0.8,
  height = 0.8,
}

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
    local end_line = vim.api.nvim_buf_line_count(float_buf) - 1
    vim.api.nvim_buf_set_lines(float_buf, -1, -1, false, lines)
    -- Autoscroll to bottom
    if float_win and vim.api.nvim_win_is_valid(float_win) then
      vim.api.nvim_win_set_cursor(float_win, {
        vim.api.nvim_buf_line_count(float_buf),
        0
      })
    end
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
end

-- Update title (e.g. when kitchen changes mid-dispatch).
function M.set_title(title)
  if float_win and vim.api.nvim_win_is_valid(float_win) then
    vim.api.nvim_win_set_option(float_win, "title", " " .. title .. " ")
  end
end

-- Return current buffer and window IDs (nil if closed).
function M.get_state()
  return float_buf, float_win
end

return M

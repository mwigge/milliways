-- milliways.nvim — The Restaurant at the End of the Universe
-- Thin Neovim plugin that calls the milliways binary for AI task routing.
-- No persistent process, no API keys, no model loading. ~0 MB memory.

local M = {}

-- Default configuration
M.config = {
  bin = "milliways",
  keybindings = true,
  leader = "<leader>m",
  float_width = 0.8,
  float_height = 0.8,
}

-- Plugin setup
function M.setup(opts)
  M.config = vim.tbl_deep_extend("force", M.config, opts or {})

  -- Register commands
  vim.api.nvim_create_user_command("Milliways", function(args)
    M.dispatch(args.args)
  end, { nargs = "?", desc = "Route a task to the best kitchen" })

  vim.api.nvim_create_user_command("MilliwaysExplain", function(args)
    M.explain(args.args)
  end, { nargs = "?", desc = "Show routing decision without executing" })

  vim.api.nvim_create_user_command("MilliwaysKitchen", function(args)
    M.pick_kitchen(args.args)
  end, { nargs = "?", desc = "Pick a kitchen, then dispatch" })

  vim.api.nvim_create_user_command("MilliwaysRecipe", function(args)
    M.pick_recipe(args.args)
  end, { nargs = "?", desc = "Pick a recipe, then dispatch" })

  vim.api.nvim_create_user_command("MilliwaysStatus", function()
    M.status()
  end, { desc = "Show kitchen availability" })

  vim.api.nvim_create_user_command("MilliwaysDetached", function()
    M.detached()
  end, { desc = "List detached dispatches" })

  -- Register keybindings
  if M.config.keybindings then
    local leader = M.config.leader
    vim.keymap.set("n", leader .. "m", ":Milliways ", { desc = "Milliways: dispatch" })
    vim.keymap.set("v", leader .. "m", function() M.dispatch_selection() end, { desc = "Milliways: dispatch selection" })
    vim.keymap.set("n", leader .. "e", ":MilliwaysExplain ", { desc = "Milliways: explain routing" })
    vim.keymap.set("n", leader .. "s", ":MilliwaysStatus<CR>", { desc = "Milliways: status" })
    vim.keymap.set("n", leader .. "r", ":MilliwaysRecipe ", { desc = "Milliways: recipe" })
    vim.keymap.set("n", leader .. "k", ":MilliwaysKitchen ", { desc = "Milliways: pick kitchen" })
  end
end

-- Dispatch a task to the best kitchen
function M.dispatch(prompt)
  if not prompt or prompt == "" then
    prompt = vim.fn.input("Milliways> ")
    if prompt == "" then return end
  end

  local context_args = M.build_context_args()
  local cmd = { M.config.bin, "--json" }
  vim.list_extend(cmd, context_args)
  table.insert(cmd, prompt)

  M.open_float("Milliways — dispatching...", true)
  M.run_async(cmd, function(output)
    local ok, result = pcall(vim.json.decode, output)
    if ok and result then
      local lines = {}
      table.insert(lines, "Kitchen: " .. (result.kitchen or "?"))
      table.insert(lines, "Tier:    " .. (result.tier or "?"))
      table.insert(lines, "")
      for line in (result.output or ""):gmatch("[^\n]+") do
        table.insert(lines, line)
      end
      M.update_float(lines)
    else
      M.update_float(vim.split(output, "\n"))
    end
  end)
end

-- Show routing decision without executing
function M.explain(prompt)
  if not prompt or prompt == "" then
    prompt = vim.fn.input("Explain> ")
    if prompt == "" then return end
  end

  local cmd = { M.config.bin, "--explain", "--json", prompt }
  M.run_sync(cmd, function(output)
    local ok, result = pcall(vim.json.decode, output)
    if ok and result then
      local lines = {
        "Kitchen: " .. (result.kitchen or "?"),
        "Reason:  " .. (result.reason or "?"),
        "Tier:    " .. (result.tier or "?"),
      }
      if result.risk and result.risk ~= "" then
        table.insert(lines, "Risk:    " .. result.risk)
      end
      M.open_float(table.concat(lines, "\n"), false)
    else
      M.open_float(output, false)
    end
  end)
end

-- Pick kitchen then dispatch
function M.pick_kitchen(prompt)
  if not prompt or prompt == "" then
    prompt = vim.fn.input("Task> ")
    if prompt == "" then return end
  end

  local kitchens = { "claude", "opencode", "gemini", "aider", "goose", "cline" }
  vim.ui.select(kitchens, { prompt = "Kitchen:" }, function(choice)
    if choice then
      local cmd = { M.config.bin, "--kitchen", choice, "--json", prompt }
      M.open_float("Milliways — " .. choice .. "...", true)
      M.run_async(cmd, function(output)
        M.update_float(vim.split(output, "\n"))
      end)
    end
  end)
end

-- Pick recipe then dispatch
function M.pick_recipe(prompt)
  if not prompt or prompt == "" then
    prompt = vim.fn.input("Task> ")
    if prompt == "" then return end
  end

  local recipes = { "implement-feature", "fix-bug", "security-audit", "explore-idea", "refactor-module" }
  vim.ui.select(recipes, { prompt = "Recipe:" }, function(choice)
    if choice then
      local cmd = { M.config.bin, "--recipe", choice, prompt }
      M.open_float("Milliways — recipe: " .. choice .. "...", true)
      M.run_async(cmd, function(output)
        M.update_float(vim.split(output, "\n"))
      end)
    end
  end)
end

-- Show kitchen status
function M.status()
  M.run_sync({ M.config.bin, "status" }, function(output)
    M.open_float(output, false)
  end)
end

-- List detached dispatches
function M.detached()
  M.run_sync({ M.config.bin, "tickets" }, function(output)
    M.open_float(output, false)
  end)
end

-- Dispatch with visual selection as context
function M.dispatch_selection()
  local start_pos = vim.fn.getpos("'<")
  local end_pos = vim.fn.getpos("'>")
  local lines = vim.api.nvim_buf_get_lines(0, start_pos[2] - 1, end_pos[2], false)
  local selection = table.concat(lines, "\n")

  local prompt = vim.fn.input("Milliways (with selection)> ")
  if prompt == "" then return end

  local full_prompt = "Context:\n```\n" .. selection .. "\n```\n\nTask: " .. prompt
  M.dispatch(full_prompt)
end

-- Build context arguments from current editor state
function M.build_context_args()
  local args = {}

  -- Current file
  local file = vim.fn.expand("%:p")
  if file and file ~= "" then
    table.insert(args, "--context-file")
    table.insert(args, file)
  end

  return args
end

-- Floating window management
local float_buf = nil
local float_win = nil

function M.open_float(content, streaming)
  -- Close existing float
  if float_win and vim.api.nvim_win_is_valid(float_win) then
    vim.api.nvim_win_close(float_win, true)
  end

  float_buf = vim.api.nvim_create_buf(false, true)
  vim.api.nvim_buf_set_option(float_buf, "bufhidden", "wipe")
  vim.api.nvim_buf_set_option(float_buf, "filetype", "markdown")

  local width = math.floor(vim.o.columns * M.config.float_width)
  local height = math.floor(vim.o.lines * M.config.float_height)
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

  if type(content) == "string" then
    content = vim.split(content, "\n")
  end
  vim.api.nvim_buf_set_lines(float_buf, 0, -1, false, content)

  -- Keybindings for the floating window
  local buf = float_buf
  vim.keymap.set("n", "q", function()
    if vim.api.nvim_win_is_valid(float_win) then
      vim.api.nvim_win_close(float_win, true)
    end
  end, { buffer = buf, desc = "Close Milliways window" })

  vim.keymap.set("n", "y", function()
    local lines = vim.api.nvim_buf_get_lines(buf, 0, -1, false)
    vim.fn.setreg("+", table.concat(lines, "\n"))
    vim.notify("Copied to clipboard", vim.log.levels.INFO)
  end, { buffer = buf, desc = "Yank output to clipboard" })
end

function M.update_float(lines)
  if float_buf and vim.api.nvim_buf_is_valid(float_buf) then
    vim.schedule(function()
      vim.api.nvim_buf_set_lines(float_buf, 0, -1, false, lines)
    end)
  end
end

-- Run command asynchronously
function M.run_async(cmd, callback)
  local output = {}
  vim.fn.jobstart(cmd, {
    stdout_buffered = true,
    on_stdout = function(_, data)
      if data then
        for _, line in ipairs(data) do
          if line ~= "" then
            table.insert(output, line)
          end
        end
      end
    end,
    on_exit = function()
      vim.schedule(function()
        callback(table.concat(output, "\n"))
      end)
    end,
  })
end

-- Run command synchronously
function M.run_sync(cmd, callback)
  local result = vim.fn.system(cmd)
  callback(result)
end

return M

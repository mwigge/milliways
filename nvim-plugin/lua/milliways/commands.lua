-- commands.lua — Dispatch commands for milliways.nvim.

local M = {}

local float = require("milliways.float")
local context = require("milliways.context")
local kitchens = require("milliways.kitchens")

local kitchen_names = { "claude", "opencode", "gemini", "minimax", "groq", "ollama", "aider", "goose", "cline" }

local function kitchen_complete(arglead, _, _)
  local matches = {}
  for _, kitchen in ipairs(kitchen_names) do
    if vim.startswith(kitchen, arglead) then
      table.insert(matches, kitchen)
    end
  end
  return matches
end

-- Module config (set by init.lua setup()).
M.config = {
  bin = "milliways",
}

M.kitchen_chain = {}
M.sticky_mode = false

function M.configure(opts)
  M.config = vim.tbl_deep_extend("force", M.config, opts or {})
end

---Update the float title to reflect kitchen lineage and shortcuts.
function M.update_float_title()
  local title = "Milliways"
  if #M.kitchen_chain > 0 then
    title = table.concat(M.kitchen_chain, " > ")
    if M.sticky_mode then
      title = title .. " | sticky"
    end
    title = title .. " | Tab recent | leader mK kitchens"
  end
  float.set_title(title)
end

-- Run a command asynchronously, streaming output line by line to the float.
-- cmd: table of command + args
-- opts: { streaming = bool, header = string }
function M.run_async(cmd, opts, callback)
  opts = opts or {}
  local output = {}
  local float_buf, float_win = float.open(opts.header or "Milliways...", opts.streaming)
  float.resume_autoscroll()
  M.update_float_title()

  vim.fn.jobstart(cmd, {
    stdout_buffered = false, -- stream line by line
    on_stdout = function(_, data)
      if data then
        for _, line in ipairs(data) do
          if line and line ~= "" then
            table.insert(output, line)
            if opts.streaming then
              float.append(line)
            end
          end
        end
      end
    end,
    on_stderr = function(_, data)
      if data then
        for _, line in ipairs(data) do
          if line and line ~= "" then
            table.insert(output, "[stderr] " .. line)
            if opts.streaming then
              float.append("[stderr] " .. line)
            end
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

  return float_buf, float_win
end

-- Run a command synchronously.
function M.run_sync(cmd, callback)
  local result = vim.fn.system(cmd)
  callback(result)
end

local function render_output(output)
  local ok, result = pcall(vim.json.decode, output)
  if ok and result then
    local lines = {}
    table.insert(lines, "Kitchen: " .. (result.kitchen or "?"))
    table.insert(lines, "Tier:    " .. (result.tier or "?"))
    table.insert(lines, "")
    for line in (result.output or ""):gmatch("[^\n]+") do
      table.insert(lines, line)
    end
    float.set_content(lines)
    return
  end
  float.set_content(vim.split(output, "\n"))
end

local function run_dispatch_command(prompt, header)
  local bundle = context.build({ include_selection = false })
  local bundle_json = vim.json.encode(bundle)

  local cmd = { M.config.bin, "--json" }
  vim.list_extend(cmd, M.build_context_args())
  if bundle_json and bundle_json ~= "{}" then
    table.insert(cmd, "--context-json")
    table.insert(cmd, bundle_json)
  end
  table.insert(cmd, prompt)

  float.add_recent(prompt, M.kitchen_chain[#M.kitchen_chain])

  M.run_async(cmd, { streaming = true, header = header }, function(output)
    render_output(output)
  end)
end

-- Dispatch a prompt to the best kitchen.
function M.dispatch(prompt)
  if not prompt or prompt == "" then
    prompt = vim.fn.input("Milliways> ")
    if prompt == "" then return end
  end

  run_dispatch_command(prompt, "Milliways...")
end

-- Show routing decision without executing.
function M.explain(prompt)
  if not prompt or prompt == "" then
    prompt = vim.fn.input("Explain> ")
    if prompt == "" then return end
  end

  local bundle = context.build({ include_selection = false })
  local bundle_json = vim.json.encode(bundle)

  local cmd = { M.config.bin, "--explain", "--json" }
  if bundle_json and bundle_json ~= "{}" then
    table.insert(cmd, "--context-json")
    table.insert(cmd, bundle_json)
  end
  table.insert(cmd, prompt)

  M.run_async(cmd, { streaming = false }, function(output)
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
      float.open(table.concat(lines, "\n"), false)
    else
      float.open(output, false)
    end
  end)
end

-- Pick a kitchen then dispatch.
function M.pick_kitchen(prompt)
  if not prompt or prompt == "" then
    prompt = vim.fn.input("Task> ")
    if prompt == "" then return end
  end

  kitchens.pick(function(choice)
    if not choice then return end

    local bundle = context.build({ include_selection = false })
    local bundle_json = vim.json.encode(bundle)

    local cmd = { M.config.bin, "--kitchen", choice, "--json" }
    if bundle_json and bundle_json ~= "{}" then
      table.insert(cmd, "--context-json")
      table.insert(cmd, bundle_json)
    end
    table.insert(cmd, prompt)

    float.add_recent(prompt, choice)
    float.open("Milliways — " .. choice .. "...", true)
    M.run_async(cmd, { streaming = true }, function(output)
      float.set_content(vim.split(output, "\n"))
    end)
  end)
end

-- Dispatch a switch command to a named kitchen.
function M.switch(kitchen)
  kitchen = vim.trim(kitchen or "")
  if kitchen == "" then
    vim.ui.select(kitchens.list_kitchen_names(), { prompt = "Switch kitchen:" }, function(choice)
      if not choice then
        return
      end
      M.switch(choice)
    end)
    return
  end

  if M.kitchen_chain[#M.kitchen_chain] ~= kitchen then
    table.insert(M.kitchen_chain, kitchen)
  end
  M.update_float_title()
  M.dispatch("switch " .. kitchen)
end

-- Toggle sticky kitchen mode.
function M.stick()
  M.sticky_mode = not M.sticky_mode
  M.update_float_title()
  M.dispatch("stick")
end

-- Reverse the most recent switch.
function M.back()
  if #M.kitchen_chain > 1 then
    table.remove(M.kitchen_chain)
  end
  M.update_float_title()
  M.dispatch("back")
end

-- Ask milliways to reroute the current task.
function M.reroute()
  M.dispatch("reroute")
end

-- Return kitchen completion candidates for user commands.
function M.kitchen_complete(arglead, cmdline, cursorpos)
  return kitchen_complete(arglead, cmdline, cursorpos)
end

-- Open a kitchen picker and switch to the selected kitchen.
function M.open_kitchens_picker()
  local function on_choice(choice)
    if not choice then
      return
    end
    M.switch(choice)
  end

  local has_telescope = pcall(require, "telescope")
  if has_telescope then
    kitchens.pick(on_choice)
    return
  end

  vim.ui.select(kitchens.list_kitchens(), {
    prompt = "Milliways Kitchens:",
    format_item = function(item)
      return item.display
    end,
  }, function(choice)
    if not choice then
      return
    end
    on_choice(choice.name)
  end)
end

-- Pick a recipe then dispatch.
function M.pick_recipe(prompt)
  if not prompt or prompt == "" then
    prompt = vim.fn.input("Task> ")
    if prompt == "" then return end
  end

  local recipes = { "implement-feature", "fix-bug", "security-audit", "explore-idea", "refactor-module" }
  vim.ui.select(recipes, { prompt = "Recipe:" }, function(choice)
    if not choice then return end

    local bundle = context.build({ include_selection = false })
    local bundle_json = vim.json.encode(bundle)

    local cmd = { M.config.bin, "--recipe", choice }
    if bundle_json and bundle_json ~= "{}" then
      table.insert(cmd, "--context-json")
      table.insert(cmd, bundle_json)
    end
    table.insert(cmd, prompt)

    float.open("Milliways — recipe: " .. choice .. "...", true)
    M.run_async(cmd, { streaming = true }, function(output)
      float.set_content(vim.split(output, "\n"))
    end)
  end)
end

-- Show kitchen availability.
function M.status()
  M.run_async({ M.config.bin, "status" }, { streaming = false }, function(output)
    float.open(output, false)
  end)
end

-- List detached dispatches.
function M.detached()
  M.run_async({ M.config.bin, "tickets" }, { streaming = false }, function(output)
    float.open(output, false)
  end)
end

-- Dispatch with visual selection as context.
function M.dispatch_selection()
  local selection = context.collect_selection()
  if not selection then
    vim.notify("No visual selection", vim.log.levels.WARN)
    return
  end

  local prompt = vim.fn.input("Milliways (with selection)> ")
  if prompt == "" then return end

  local full_prompt = "Context:\n```\n" .. selection.text .. "\n```\n\nTask: " .. prompt
  M.dispatch(full_prompt)
end

-- Build legacy context arguments (--context-file).
function M.build_context_args()
  local args = {}
  local file = vim.fn.expand("%:p")
  if file and file ~= "" then
    table.insert(args, "--context-file")
    table.insert(args, file)
  end
  return args
end

return M

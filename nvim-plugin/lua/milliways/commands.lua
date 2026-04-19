-- commands.lua — Dispatch commands for milliways.nvim.

local M = {}

local float = require("milliways.float")
local context = require("milliways.context")
local kitchens = require("milliways.kitchens")

-- Module config (set by init.lua setup()).
M.config = {
  bin = "milliways",
}

function M.configure(opts)
  M.config = vim.tbl_deep_extend("force", M.config, opts or {})
end

-- Run a command asynchronously, streaming output line by line to the float.
-- cmd: table of command + args
-- opts: { streaming = bool, header = string }
function M.run_async(cmd, opts, callback)
  opts = opts or {}
  local output = {}
  local float_buf, float_win = float.open(opts.header or "Milliways...", opts.streaming)

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

-- Dispatch a prompt to the best kitchen.
function M.dispatch(prompt)
  if not prompt or prompt == "" then
    prompt = vim.fn.input("Milliways> ")
    if prompt == "" then return end
  end

  -- Build context bundle
  local bundle = context.build({ include_selection = false })
  local bundle_json = vim.json.encode(bundle)

  local cmd = { M.config.bin, "--json" }
  vim.list_extend(cmd, M.build_context_args())
  if bundle_json and bundle_json ~= "{}" then
    table.insert(cmd, "--context-json")
    table.insert(cmd, bundle_json)
  end
  table.insert(cmd, prompt)

  run_async(cmd, { streaming = true }, function(output)
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
    else
      float.set_content(vim.split(output, "\n"))
    end
  end)
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

  run_async(cmd, { streaming = false }, function(output)
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

    float.open("Milliways — " .. choice .. "...", true)
    run_async(cmd, { streaming = true }, function(output)
      float.set_content(vim.split(output, "\n"))
    end)
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
    run_async(cmd, { streaming = true }, function(output)
      float.set_content(vim.split(output, "\n"))
    end)
  end)
end

-- Show kitchen availability.
function M.status()
  run_async({ M.config.bin, "status" }, { streaming = false }, function(output)
    float.open(output, false)
  end)
end

-- List detached dispatches.
function M.detached()
  run_async({ M.config.bin, "tickets" }, { streaming = false }, function(output)
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

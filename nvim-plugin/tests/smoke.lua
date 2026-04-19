local ok_commands, commands = pcall(require, "milliways.commands")
local ok_kitchens, kitchens = pcall(require, "milliways.kitchens")
local ok_context, context = pcall(require, "milliways.context")
local ok_float, float = pcall(require, "milliways.float")

assert(ok_commands and ok_kitchens and ok_context and ok_float, "module load failed")

assert(type(float.resume_autoscroll) == "function", "float.resume_autoscroll missing")
assert(type(float.add_recent) == "function", "float.add_recent missing")
assert(type(float.cycle_recent) == "function", "float.cycle_recent missing")
assert(type(commands.update_float_title) == "function", "commands.update_float_title missing")

float.recent_dispatches = {}
float.recent_idx = 0

float.add_recent("first prompt", "claude")
float.add_recent("second prompt", "codex")
float.add_recent("third prompt", "gemini")
float.add_recent("fourth prompt", "opencode")

assert(#float.recent_dispatches == 3, "recent dispatches should be capped at 3")
assert(float.recent_dispatches[1].prompt == "fourth prompt", "latest recent should be first")
assert(float.recent_dispatches[3].prompt == "second prompt", "oldest retained recent mismatch")

local buf, win = float.open({ "one", "two", "three" }, true)
vim.wait(100, function()
  return vim.api.nvim_buf_line_count(buf) == 3
end)

float.append("four")
vim.wait(100, function()
  return vim.api.nvim_buf_line_count(buf) == 4
end)
assert(vim.api.nvim_win_get_cursor(win)[1] == 4, "append should autoscroll before user movement")

vim.api.nvim_set_current_win(win)
vim.api.nvim_feedkeys("k", "xt", false)
vim.wait(100, function()
  return vim.api.nvim_win_get_cursor(win)[1] == 3
end)

float.append("five")
vim.wait(100, function()
  return vim.api.nvim_buf_line_count(buf) == 5
end)
assert(vim.api.nvim_win_get_cursor(win)[1] == 3, "append should not autoscroll after user movement")

float.resume_autoscroll()
float.append("six")
vim.wait(100, function()
  return vim.api.nvim_buf_line_count(buf) == 6
end)
assert(vim.api.nvim_win_get_cursor(win)[1] == 6, "resume_autoscroll should restore autoscroll")

commands.kitchen_chain = { "claude", "codex" }
commands.sticky_mode = true
commands.update_float_title()
local title = vim.api.nvim_win_get_config(win).title
if type(title) == "table" then
  title = title[1][1]
end
assert(title == " claude > codex | sticky | Tab recent | leader mK kitchens ", "unexpected float title")

float.cycle_recent()
vim.wait(100, function()
  local lines = vim.api.nvim_buf_get_lines(buf, 0, 3, false)
  return #lines >= 3 and lines[1]:match("^--- Recent:") ~= nil
end)

local preview = vim.api.nvim_buf_get_lines(buf, 0, 3, false)
assert(preview[1] == "--- Recent: fourth prompt", "recent preview prompt mismatch")
assert(preview[2] == "Kitchen: opencode", "recent preview kitchen mismatch")

assert(ok_context, "context module load failed")
assert(type(context.build) == "function", "context.build missing")
assert(type(context.collect_buffer) == "function", "context.collect_buffer missing")
assert(type(context.collect_cursor) == "function", "context.collect_cursor missing")
assert(type(context.collect_selection) == "function", "context.collect_selection missing")
assert(type(context.collect_project) == "function", "context.collect_project missing")
assert(type(context.collect_lsp) == "function", "context.collect_lsp missing")
assert(type(context.collect_git) == "function", "context.collect_git missing")

local bundle = context.build({})
assert(bundle ~= nil, "build should return a table")
assert(bundle.schema_version ~= nil, "bundle must have schema_version")

assert(ok_kitchens, "kitchens module load failed")
assert(type(kitchens.list_kitchen_names) == "function", "list_kitchen_names missing")

local names = kitchens.list_kitchen_names()
assert(type(names) == "table", "list_kitchen_names should return a table")

float.close()

print("All modules loaded OK")

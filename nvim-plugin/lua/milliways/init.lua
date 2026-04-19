-- milliways.nvim — The Restaurant at the End of the Universe
-- L2-aware Neovim plugin that calls the milliways binary for AI task routing.
-- No persistent process, no API keys, no model loading.

local M = {}

M.config = {
  bin = "milliways",
  keybindings = true,
  leader = "<leader>m",
  float_width = 0.8,
  float_height = 0.8,
  context_timeout_ms = 15,
  context_budget_kb = 64,
  -- Per-collector opt-in/opt-out
  collectors = {
    buffer = true,
    cursor = true,
    selection = "auto",  -- true, false, or "auto" (only in visual mode)
    lsp = true,
    git = true,
    project = true,
    quickfix = "auto",
    loclist = "auto",
  },
}

function M.setup(opts)
  M.config = vim.tbl_deep_extend("force", M.config, opts or {})

  -- Configure submodules
  local context = require("milliways.context")
  context.configure({ timeout_ms = M.config.context_timeout_ms })

  local float = require("milliways.float")
  float.configure({
    float_width = M.config.float_width,
    float_height = M.config.float_height,
  })

  local commands = require("milliways.commands")
  commands.configure({ bin = M.config.bin })

  -- Register commands
  vim.api.nvim_create_user_command("Milliways", function(args)
    commands.dispatch(args.args)
  end, { nargs = "?", desc = "Route a task to the best kitchen" })

  vim.api.nvim_create_user_command("MilliwaysExplain", function(args)
    commands.explain(args.args)
  end, { nargs = "?", desc = "Show routing decision without executing" })

  vim.api.nvim_create_user_command("MilliwaysKitchen", function(args)
    commands.pick_kitchen(args.args)
  end, { nargs = "?", desc = "Pick a kitchen, then dispatch" })

  vim.api.nvim_create_user_command("MilliwaysRecipe", function(args)
    commands.pick_recipe(args.args)
  end, { nargs = "?", desc = "Pick a recipe, then dispatch" })

  vim.api.nvim_create_user_command("MilliwaysStatus", function()
    commands.status()
  end, { desc = "Show kitchen availability" })

  vim.api.nvim_create_user_command("MilliwaysDetached", function()
    commands.detached()
  end, { desc = "List detached dispatches" })

  -- Register keybindings
  if M.config.keybindings then
    local leader = M.config.leader
    vim.keymap.set("n", leader .. "m", ":Milliways ", { desc = "Milliways: dispatch" })
    vim.keymap.set("v", leader .. "m", function() commands.dispatch_selection() end,
      { desc = "Milliways: dispatch selection" })
    vim.keymap.set("n", leader .. "e", ":MilliwaysExplain ", { desc = "Milliways: explain routing" })
    vim.keymap.set("n", leader .. "s", ":MilliwaysStatus<CR>", { desc = "Milliways: status" })
    vim.keymap.set("n", leader .. "r", ":MilliwaysRecipe ", { desc = "Milliways: recipe" })
    vim.keymap.set("n", leader .. "k", ":MilliwaysKitchen ", { desc = "Milliways: pick kitchen" })
  end
end

-- Expose submodules for advanced users.
M.context = require("milliways.context")
M.commands = require("milliways.commands")
M.float = require("milliways.float")
M.kitchens = require("milliways.kitchens")

return M

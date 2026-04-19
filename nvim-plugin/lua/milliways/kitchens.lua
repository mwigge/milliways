-- kitchens.lua — Kitchen picker with optional Telescope integration.

local M = {}

-- Determine if Telescope is available.
local has_telescope, telescope = pcall(require, "telescope")
if has_telescope then
  -- Try to load telescope.load_extension if available
end

-- Pick a kitchen using Telescope (if available) or vim.ui.select.
-- callback(kitchen_name | nil)
function M.pick(callback)
  if has_telescope then
    M.pick_telescope(callback)
  else
    M.pick_vim_ui(callback)
  end
end

-- Telescope-based picker.
function M.pick_telescope(callback)
  local action_state = require("telescope.actions.state")
  local actions = require("telescope.actions")

  -- Try to load the picker with actions
  local ok, picker = pcall(function()
    return require("telescope.pickers").new({}, {
      prompt_title = "Milliways Kitchen",
      finder = require("telescope.finders").new_table({
        results = M.list_kitchens(),
        entry_maker = function(entry)
          return {
            value = entry.name,
            display = entry.display,
            ordinal = entry.name,
          }
        end,
      }),
      sorter = require("telescope.config").values.generic_sorter(),
      attach_mappings = function(prompt_bufnr, map)
        actions.select_default:replace(function()
          local selection = action_state.get_selected_entry()
          actions.close(prompt_bufnr)
          if selection then
            callback(selection.value)
          else
            callback(nil)
          end
        end)
        return true
      end,
    })
  end)

  if ok and picker then
    picker:find()
  else
    -- Fallback if Telescope picker creation fails
    M.pick_vim_ui(callback)
  end
end

-- vim.ui.select-based picker.
function M.pick_vim_ui(callback)
  local items = M.list_kitchens()
  vim.ui.select(items, {
    prompt = "Kitchen:",
    format_item = function(item)
      return item.display
    end,
  }, function(choice)
    if choice then
      callback(choice.name)
    else
      callback(nil)
    end
  end)
end

-- List available kitchens with display strings.
-- Each entry: { name = "claude", display = "claude (cloud)", tier = "cloud" }
function M.list_kitchens()
  -- Default list; can be overridden by reading from milliways status.
  return {
    { name = "claude",   display = "claude (cloud)",    tier = "cloud"  },
    { name = "opencode", display = "opencode (local)",  tier = "local"  },
    { name = "gemini",   display = "gemini (free)",     tier = "free"   },
    { name = "aider",    display = "aider (cloud)",     tier = "cloud"  },
    { name = "goose",   display = "goose (local)",     tier = "local"  },
    { name = "cline",   display = "cline (cloud)",      tier = "cloud"  },
    { name = "minimax", display = "minimax (cloud)",    tier = "cloud"  },
    { name = "groq",    display = "groq (free)",        tier = "free"   },
    { name = "ollama",  display = "ollama (local)",     tier = "local"  },
  }
end

-- List kitchen names only.
function M.list_kitchen_names()
  local names = {}
  for _, kitchen in ipairs(M.list_kitchens()) do
    table.insert(names, kitchen.name)
  end
  return names
end

return M

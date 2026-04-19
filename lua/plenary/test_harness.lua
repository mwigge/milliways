local M = {}

local function list_test_files(test_dir)
  local pattern = test_dir .. "/*.lua"
  local files = vim.fn.glob(pattern, false, true)
  table.sort(files)

  local filtered = {}
  for _, file in ipairs(files) do
    if not file:match("/minimal_init%.lua$") then
      table.insert(filtered, file)
    end
  end

  return filtered
end

function M.test_nvim(test_dir, opts)
  local options = opts or {}

  local ok, err = xpcall(function()
    if options.minimal_init then
      dofile(options.minimal_init)
    end

    local test_files = list_test_files(test_dir)
    for _, file in ipairs(test_files) do
      dofile(file)
    end
  end, debug.traceback)

  if not ok then
    vim.api.nvim_err_writeln(err)
    vim.cmd("cquit 1")
    return
  end

  vim.cmd("qa!")
end

return M

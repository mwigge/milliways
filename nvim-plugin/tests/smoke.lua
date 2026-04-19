local ok_commands, _ = pcall(require, "milliways.commands")
local ok_kitchens, _ = pcall(require, "milliways.kitchens")
local ok_context, _ = pcall(require, "milliways.context")
local ok_float, _ = pcall(require, "milliways.float")

assert(ok_commands and ok_kitchens and ok_context and ok_float, "module load failed")

print("All modules loaded OK")

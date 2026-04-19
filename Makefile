VERSION := $(shell git describe --tags --always --dirty 2>/dev/null)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: smoke plugin-test install mempalace-dev mempalace-test

mempalace-dev:
	pip install -e third_party/mempalace_milliways/[dev]
	@echo "Installed mempalace-milliways. Required env vars for milliways:"
	@echo "  export MILLIWAYS_MEMPALACE_MCP_CMD='python3.14 -m mempalace.mcp_server'"
	@echo "  export MEMPALACE_PALACE_PATH='$$HOME/.local/share/mempalace'"

mempalace-test:
	PYTHONPATH=src third_party/mempalace_milliways/.venv/bin/python -m pytest tests/ --tb=short -q


smoke:
	go build -ldflags "$(LDFLAGS)" -o $(TMPDIR)/milliways ./cmd/milliways
	VERSION=$(VERSION) scripts/smoke.sh

# CI note: add `make plugin-test` after the Go smoke step.
plugin-test:
	@which nvim >/dev/null 2>&1 || { printf '%s\n' 'nvim not found — install Neovim to run plugin tests' >&2; exit 1; }
	nvim --headless -u NONE \
	  --cmd "set rtp+=." \
	  --cmd "set rtp+=nvim-plugin" \
	  -c "lua require('plenary.test_harness').test_nvim('nvim-plugin/tests', { minimal_init = 'nvim-plugin/tests/minimal_init.lua' })" \
	  2>&1

install:
	go build -ldflags "$(LDFLAGS)" -o ~/.local/bin/milliways ./cmd/milliways

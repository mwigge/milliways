VERSION := $(shell git describe --tags --always --dirty 2>/dev/null)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: smoke plugin-test install

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

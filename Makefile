VERSION := $(shell git describe --tags --always --dirty 2>/dev/null)
LDFLAGS := -X main.version=$(VERSION)

PREFIX ?= $(HOME)/.local
BIN := $(PREFIX)/bin

.PHONY: smoke plugin-test install mempalace-dev mempalace-test \
        all term daemon ctl repl gen-rpc clean-rpc

# ---------------------------------------------------------------------------
# milliways-emulator-fork build targets
# ---------------------------------------------------------------------------

# all: build every artefact (Rust terminal + Go daemon + ctl + legacy repl).
all: term daemon ctl repl

# term: build the wezterm fork into ~/.local/bin/milliways-term.
# Requires: rust toolchain, crates/milliways-term/ populated by `git subtree add`.
# Crate name remains `wezterm-gui` (used as `-p wezterm-gui`); the produced
# binary is renamed to `milliways-term` via [[bin]] block in
# crates/milliways-term/wezterm-gui/Cargo.toml (see PATCHES.md).
term:
	@if [ ! -d crates/milliways-term ]; then \
		echo "crates/milliways-term not present — run Phase 2 (subtree-import wezterm) first."; \
		exit 1; \
	fi
	cargo build --release \
		--manifest-path crates/milliways-term/Cargo.toml \
		-p wezterm-gui
	install -m 0755 crates/milliways-term/target/release/milliways-term $(BIN)/milliways-term

# daemon: build milliwaysd (Go).
daemon:
	@if [ ! -d cmd/milliwaysd ]; then \
		echo "cmd/milliwaysd not present — Phase 1 not yet implemented."; \
		exit 1; \
	fi
	go build -ldflags "$(LDFLAGS)" -o $(BIN)/milliwaysd ./cmd/milliwaysd

# ctl: build milliwaysctl (Go thin client).
ctl:
	@if [ ! -d cmd/milliwaysctl ]; then \
		echo "cmd/milliwaysctl not present — Phase 1 not yet implemented."; \
		exit 1; \
	fi
	go build -ldflags "$(LDFLAGS)" -o $(BIN)/milliwaysctl ./cmd/milliwaysctl

# repl: build the legacy in-host REPL (`milliways --repl`).
repl:
	go build -ldflags "$(LDFLAGS)" -o $(BIN)/milliways ./cmd/milliways

# gen-rpc: regenerate Go and Rust RPC types from proto/milliways.json.
gen-rpc:
	scripts/gen-rpc-types.sh

# clean-rpc: remove generated RPC types so a re-run of gen-rpc starts clean.
clean-rpc:
	rm -f internal/rpc/types.go
	rm -f crates/milliways-term/milliways/src/rpc/types.rs

# ---------------------------------------------------------------------------
# Pre-existing legacy targets (preserved for the --repl path).
# ---------------------------------------------------------------------------

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

install: repl

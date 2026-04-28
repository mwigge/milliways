VERSION := $(shell git describe --tags --always --dirty 2>/dev/null)
LDFLAGS := -X main.version=$(VERSION)

PREFIX ?= $(HOME)/.local
BIN := $(PREFIX)/bin

.PHONY: smoke plugin-test install mempalace-dev mempalace-test \
        all term daemon ctl repl gen-rpc clean-rpc \
        bundle-macos release

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

# ---------------------------------------------------------------------------
# macOS app bundle — assembles MilliWays.app and zips it for release.
#
# Requires: the patched wezterm-gui and wezterm-mux-server binaries.
# Source priority:
#   1. WEZTERM_BIN_DIR env var (CI sets this to a downloaded artifact dir)
#   2. /Applications/MilliWays.app/Contents/MacOS  (dev machine shortcut)
# ---------------------------------------------------------------------------
BUNDLE_DIR  := $(CURDIR)/bundle/macos
DIST_DIR    := $(CURDIR)/dist
APP_NAME    := MilliWays.app
APP_DIR     := $(DIST_DIR)/$(APP_NAME)
WEZTERM_SRC ?= /Applications/MilliWays.app/Contents/MacOS

bundle-macos: repl
	@echo "==> Assembling $(APP_NAME) ($(VERSION))"
	@[ -d "$(WEZTERM_SRC)" ] || { \
		echo "ERROR: wezterm binaries not found."; \
		echo "  Set WEZTERM_BIN_DIR=<dir containing wezterm-gui and wezterm-mux-server>"; \
		echo "  or install MilliWays.app to /Applications first."; \
		exit 1; \
	}
	rm -rf "$(APP_DIR)"
	mkdir -p "$(APP_DIR)/Contents/MacOS" "$(APP_DIR)/Contents/Resources"
	# wezterm binaries
	cp "$(WEZTERM_SRC)/wezterm-gui"        "$(APP_DIR)/Contents/MacOS/"
	cp "$(WEZTERM_SRC)/wezterm-mux-server" "$(APP_DIR)/Contents/MacOS/"
	chmod +x "$(APP_DIR)/Contents/MacOS/wezterm-gui" \
	          "$(APP_DIR)/Contents/MacOS/wezterm-mux-server"
	# milliways binary (built by `repl` target above)
	cp "$(BIN)/milliways" "$(APP_DIR)/Contents/MacOS/"
	# resources
	cp "$(BUNDLE_DIR)/milliways.icns" "$(APP_DIR)/Contents/Resources/"
	sed "s/__VERSION__/$(VERSION)/g" "$(BUNDLE_DIR)/Info.plist" \
		> "$(APP_DIR)/Contents/Info.plist"
	# strip quarantine so Gatekeeper doesn't block unsigned binaries
	xattr -cr "$(APP_DIR)" 2>/dev/null || true
	# zip for upload
	cd "$(DIST_DIR)" && zip -qr "$(APP_NAME).zip" "$(APP_NAME)"
	@echo "==> $(DIST_DIR)/$(APP_NAME).zip"

# ---------------------------------------------------------------------------
# release — tag, build, and publish a GitHub Release.
#
# Usage: make release TAG=v0.4.14
#   Requires: gh CLI authenticated, clean working tree.
# ---------------------------------------------------------------------------
TAG ?= $(VERSION)

release: bundle-macos
	@command -v gh >/dev/null 2>&1 || { echo "gh CLI required: brew install gh"; exit 1; }
	@git diff --quiet HEAD || { echo "Working tree is dirty — commit or stash first"; exit 1; }
	@echo "==> Building cross-platform milliways binaries"
	GOOS=darwin  GOARCH=arm64  go build -ldflags "$(LDFLAGS)" \
		-o "$(DIST_DIR)/milliways_darwin_arm64"  ./cmd/milliways
	GOOS=darwin  GOARCH=amd64  go build -ldflags "$(LDFLAGS)" \
		-o "$(DIST_DIR)/milliways_darwin_amd64"  ./cmd/milliways
	GOOS=linux   GOARCH=amd64  go build -ldflags "$(LDFLAGS)" \
		-o "$(DIST_DIR)/milliways_linux_amd64"   ./cmd/milliways
	GOOS=linux   GOARCH=arm64  go build -ldflags "$(LDFLAGS)" \
		-o "$(DIST_DIR)/milliways_linux_arm64"   ./cmd/milliways
	@echo "==> Creating GitHub Release $(TAG)"
	gh release create "$(TAG)" \
		--title "milliways $(TAG)" \
		--notes-file <(git log --pretty=format:'- %s' $$(git describe --tags --abbrev=0 HEAD^)..HEAD 2>/dev/null || echo "See CHANGELOG.md") \
		"$(DIST_DIR)/milliways_darwin_arm64" \
		"$(DIST_DIR)/milliways_darwin_amd64" \
		"$(DIST_DIR)/milliways_linux_amd64" \
		"$(DIST_DIR)/milliways_linux_arm64" \
		"$(DIST_DIR)/$(APP_NAME).zip"
	@echo "==> Released $(TAG)"

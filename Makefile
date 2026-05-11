SHELL := /bin/bash

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null)
LDFLAGS := -X main.version=$(VERSION)
# Suppress qualifier warning from sqlite3-binding.c in go-sqlite3.
# Linux gcc uses -Wno-discarded-qualifiers; macOS clang uses -Wno-ignored-qualifiers.
ifeq ($(shell uname),Darwin)
export CGO_CFLAGS += -Wno-ignored-qualifiers
else
export CGO_CFLAGS += -Wno-discarded-qualifiers
endif

PREFIX ?= $(HOME)/.local
BIN := $(PREFIX)/bin
DATADIR := $(PREFIX)/share

.PHONY: smoke plugin-test install mempalace-dev mempalace-test \
        all term daemon ctl repl gen-rpc clean-rpc \
        bundle-macos bundle-linux install-linux-app \
        build-linux-amd64 smoke-linux-install release

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
	PYTHONPATH=third_party/mempalace_milliways/src third_party/mempalace_milliways/.venv/bin/python -m pytest third_party/mempalace_milliways/tests/ --tb=short -q


smoke:
	go build -ldflags "$(LDFLAGS)" -o $(TMPDIR)/milliways ./cmd/milliways
	VERSION=$(VERSION) scripts/smoke.sh

build-linux-amd64:
	VERSION=$(VERSION) scripts/build-linux-amd64.sh

smoke-linux-install:
	MILLIWAYS_VERSION=$(VERSION) scripts/smoke-linux-install.sh

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
LINUX_BUNDLE_DIR := $(CURDIR)/bundle/linux
LINUX_APPDIR     := $(DIST_DIR)/MilliWays-linux-amd64
LINUX_TERM_SRC  ?= $(WEZTERM_SRC)

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
	# Strip ONLY quarantine (not all xattrs — `xattr -cr` would wipe the
	# resource manifest the code signature depends on, leaving the bundle
	# unlaunchable with "code has no resources but signature indicates
	# they must be present").
	find "$(APP_DIR)" -exec xattr -d com.apple.quarantine {} \; 2>/dev/null || true
	# Ad-hoc re-sign so Gatekeeper accepts the freshly-assembled bundle.
	# Without this the cp's into MacOS/ invalidate any inherited signature
	# and macOS refuses to launch the app silently.
	codesign --force --deep --sign - "$(APP_DIR)" 2>/dev/null || \
		echo "WARN: codesign failed; bundle may not launch on macOS without ad-hoc sign"
	codesign --verify --verbose=0 "$(APP_DIR)" 2>&1 | head -3 || true
	# zip for upload
	cd "$(DIST_DIR)" && zip -qr "$(APP_NAME).zip" "$(APP_NAME)"
	@echo "==> $(DIST_DIR)/$(APP_NAME).zip"

# ---------------------------------------------------------------------------
# Linux desktop app bundle — the freedesktop.org equivalent of MilliWays.app.
#
# Requires patched terminal binaries:
#   - milliways-term (or wezterm-gui, which is installed as milliways-term)
#   - wezterm-mux-server
#
# Source priority:
#   1. LINUX_TERM_SRC env var / make var
#   2. WEZTERM_SRC env var / make var
# ---------------------------------------------------------------------------
bundle-linux: repl
	@echo "==> Assembling MilliWays Linux desktop app ($(VERSION))"
	@[ -d "$(LINUX_TERM_SRC)" ] || { \
		echo "ERROR: terminal binaries not found."; \
		echo "  Set LINUX_TERM_SRC=<dir containing milliways-term or wezterm-gui and wezterm-mux-server>"; \
		exit 1; \
	}
	rm -rf "$(LINUX_APPDIR)"
	mkdir -p "$(LINUX_APPDIR)/bin" \
	         "$(LINUX_APPDIR)/share/applications" \
	         "$(LINUX_APPDIR)/share/icons/hicolor/scalable/apps" \
	         "$(LINUX_APPDIR)/share/milliways"
	if [ -x "$(LINUX_TERM_SRC)/milliways-term" ]; then \
		cp "$(LINUX_TERM_SRC)/milliways-term" "$(LINUX_APPDIR)/bin/milliways-term"; \
	else \
		cp "$(LINUX_TERM_SRC)/wezterm-gui" "$(LINUX_APPDIR)/bin/milliways-term"; \
	fi
	cp "$(LINUX_TERM_SRC)/wezterm-mux-server" "$(LINUX_APPDIR)/bin/"
	cp "$(BIN)/milliways" "$(LINUX_APPDIR)/bin/"
	cp "$(LINUX_BUNDLE_DIR)/dev.milliways.MilliWays.desktop" \
	   "$(LINUX_APPDIR)/share/applications/"
	cp "$(CURDIR)/assets/milliways.svg" \
	   "$(LINUX_APPDIR)/share/icons/hicolor/scalable/apps/dev.milliways.MilliWays.svg"
	cp "$(CURDIR)/cmd/milliwaysctl/milliways.lua" \
	   "$(LINUX_APPDIR)/share/milliways/wezterm.lua"
	chmod +x "$(LINUX_APPDIR)/bin/milliways-term" \
	         "$(LINUX_APPDIR)/bin/wezterm-mux-server" \
	         "$(LINUX_APPDIR)/bin/milliways"
	cd "$(DIST_DIR)" && tar -czf "MilliWays-linux-amd64.tar.gz" "MilliWays-linux-amd64"
	@echo "==> $(DIST_DIR)/MilliWays-linux-amd64.tar.gz"

install-linux-app: bundle-linux
	@echo "==> Installing MilliWays desktop app to $(PREFIX)"
	install -Dm755 "$(LINUX_APPDIR)/bin/milliways-term" "$(BIN)/milliways-term"
	install -Dm755 "$(LINUX_APPDIR)/bin/wezterm-mux-server" "$(BIN)/wezterm-mux-server"
	sed -e "s|^Exec=.*|Exec=$(BIN)/milliways-term|" \
	    -e "s|^TryExec=.*|TryExec=$(BIN)/milliways-term|" \
	    "$(LINUX_APPDIR)/share/applications/dev.milliways.MilliWays.desktop" \
	    > "$(LINUX_APPDIR)/share/applications/dev.milliways.MilliWays.local.desktop"
	install -Dm644 "$(LINUX_APPDIR)/share/applications/dev.milliways.MilliWays.local.desktop" \
		"$(DATADIR)/applications/dev.milliways.MilliWays.desktop"
	install -Dm644 "$(LINUX_APPDIR)/share/icons/hicolor/scalable/apps/dev.milliways.MilliWays.svg" \
		"$(DATADIR)/icons/hicolor/scalable/apps/dev.milliways.MilliWays.svg"
	install -Dm644 "$(LINUX_APPDIR)/share/milliways/wezterm.lua" \
		"$(DATADIR)/milliways/wezterm.lua"
	@command -v update-desktop-database >/dev/null 2>&1 && \
		update-desktop-database "$(DATADIR)/applications" || true
	@command -v gtk-update-icon-cache >/dev/null 2>&1 && \
		gtk-update-icon-cache -q "$(DATADIR)/icons/hicolor" || true
	@echo "==> MilliWays desktop app installed"

# install-macos: replace /Applications/MilliWays.app with the freshly-built
# bundle. Uses a temp dir + atomic rename instead of `cp -R` / `ditto` over
# an existing bundle (which both produce a nested MilliWays.app inside
# itself when the destination already exists). Re-signs after the move
# (the move itself doesn't break the signature, but if the user has a
# stale broken sign in place we want to leave them with a valid one).
install-macos: bundle-macos
	@echo "==> Installing $(APP_NAME) to /Applications"
	@if [ -d /Applications/$(APP_NAME) ]; then \
		echo "  → archiving existing /Applications/$(APP_NAME) to /tmp/$(APP_NAME).old.$$$$"; \
		mv /Applications/$(APP_NAME) /tmp/$(APP_NAME).old.$$$$; \
	fi
	cp -R "$(APP_DIR)" /Applications/
	codesign --force --deep --sign - /Applications/$(APP_NAME) 2>/dev/null || true
	codesign --verify --verbose=0 /Applications/$(APP_NAME) 2>&1 | head -3 || true
	@echo "==> /Applications/$(APP_NAME) installed (version $(VERSION))"

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
	@echo "==> Building cross-platform binaries (milliways, milliwaysd, milliwaysctl × 4 targets)"
	@for triple in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64; do \
		os=$${triple%/*} ; arch=$${triple#*/} ; \
		for bin in milliways milliwaysd milliwaysctl ; do \
			echo "  → $${bin}_$${os}_$${arch}" ; \
			GOOS=$$os GOARCH=$$arch go build -ldflags "$(LDFLAGS)" \
				-o "$(DIST_DIR)/$${bin}_$${os}_$${arch}" "./cmd/$${bin}" || exit 1 ; \
		done ; \
	done
	@echo "==> Creating GitHub Release $(TAG)"
	gh release create "$(TAG)" \
		--title "milliways $(TAG)" \
		--notes-file <(git log --pretty=format:'- %s' $$(git describe --tags --abbrev=0 HEAD^)..HEAD 2>/dev/null || echo "See CHANGELOG.md") \
		"$(DIST_DIR)/milliways_darwin_arm64" \
		"$(DIST_DIR)/milliways_darwin_amd64" \
		"$(DIST_DIR)/milliways_linux_amd64" \
		"$(DIST_DIR)/milliways_linux_arm64" \
		"$(DIST_DIR)/milliwaysd_darwin_arm64" \
		"$(DIST_DIR)/milliwaysd_darwin_amd64" \
		"$(DIST_DIR)/milliwaysd_linux_amd64" \
		"$(DIST_DIR)/milliwaysd_linux_arm64" \
		"$(DIST_DIR)/milliwaysctl_darwin_arm64" \
		"$(DIST_DIR)/milliwaysctl_darwin_amd64" \
		"$(DIST_DIR)/milliwaysctl_linux_amd64" \
		"$(DIST_DIR)/milliwaysctl_linux_arm64" \
		"$(DIST_DIR)/$(APP_NAME).zip"
	@echo "==> Released $(TAG)"

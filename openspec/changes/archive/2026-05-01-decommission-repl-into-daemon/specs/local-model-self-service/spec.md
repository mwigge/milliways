## ADDED Requirements

### Requirement: milliwaysctl local subcommand tree

`milliwaysctl` SHALL expose a `local` verb tree covering server installation, model download, backend switching, model setup, and discovery, so users can complete the full local-model bootstrap without leaving the milliways terminal.

#### Scenario: install-server delegates to the existing script

- **WHEN** the user runs `milliwaysctl local install-server`
- **THEN** `scripts/install_local.sh` runs to completion
- **AND** stdout/stderr are streamed to the user's terminal in real time
- **AND** the script's exit code is propagated as the ctl exit code

#### Scenario: install-swap delegates to the existing script and respects HOT_MODE

- **WHEN** the user runs `milliwaysctl local install-swap --hot`
- **THEN** `scripts/install_local_swap.sh` is invoked with `HOT_MODE=1`
- **AND** without the flag, the script runs with `HOT_MODE=0` (the script's default)

#### Scenario: list-models reads from the configured backend

- **WHEN** the user runs `milliwaysctl local list-models`
- **THEN** the command issues `GET $MILLIWAYS_LOCAL_ENDPOINT/models` (default `http://localhost:8765/v1/models`)
- **AND** prints one model ID per line on stdout
- **AND** returns a non-zero exit code with a clear error message when the backend is unreachable

#### Scenario: switch-server writes the endpoint configuration

- **WHEN** the user runs `milliwaysctl local switch-server llama-swap`
- **THEN** `$HOME/.config/milliways/local.env` is written with `MILLIWAYS_LOCAL_ENDPOINT=http://127.0.0.1:8765/v1`
- **AND** the resolved endpoint URL is printed
- **AND** unknown backend kinds return a non-zero exit with the supported set listed

#### Scenario: download-model fetches a GGUF and reports its path

- **WHEN** the user runs `milliwaysctl local download-model unsloth/Qwen2.5-Coder-7B-Instruct-GGUF --quant Q4_K_M --alias qwen2.5-coder-7b`
- **THEN** the matching GGUF is curled into `$HOME/.local/share/milliways/models/qwen2.5-coder-7b.gguf`
- **AND** the absolute path is printed on success
- **AND** an existing file with the same target path is skipped unless `--force` is passed

#### Scenario: setup-model composes download + registration + verification

- **WHEN** the user runs `milliwaysctl local setup-model unsloth/Qwen2.5-Coder-7B-Instruct-GGUF`
- **THEN** the model is downloaded (if not already cached)
- **AND** an entry is added to `$HOME/.config/milliways/llama-swap.yaml` if it does not already contain the alias
- **AND** the entry insertion is idempotent (running twice does not duplicate the model block)
- **AND** the command verifies the model is reachable via `list-models` after setup

### Requirement: Generic slash-command dispatcher in milliways-term

The wezterm Lua integration SHALL expose a generic dispatcher that catches `/<word> [args...]` typed in any milliways-term tab and runs `milliwaysctl <word> [args...]`, streaming the subprocess output back into the tab.

#### Scenario: typing /<word> runs the matching ctl subcommand

- **WHEN** the user types `/local-list-models` in a milliways-term tab
- **THEN** wezterm runs `milliwaysctl local-list-models` (or `milliwaysctl local list-models` per the dispatcher's configured verb-splitting rule)
- **AND** the subprocess output is streamed inline in the same tab
- **AND** the subprocess exit code is reflected in a status indicator (badge, color, or trailing line)

#### Scenario: unknown slash command surfaces the ctl error

- **WHEN** the user types `/notarealcommand`
- **THEN** the dispatcher invokes `milliwaysctl notarealcommand`
- **AND** ctl's `unknown subcommand` error is shown in the tab
- **AND** the dispatcher does not crash the wezterm session

#### Scenario: dispatcher does not interfere with normal terminal input

- **WHEN** the user types text that does not begin with `/` (e.g., a regular shell command)
- **THEN** wezterm processes the input as a normal terminal keystroke
- **AND** no ctl invocation is triggered

### Requirement: One source of truth for verb implementations

Every slash command surfaced by the wezterm dispatcher SHALL resolve to a `milliwaysctl` subcommand implementation. The wezterm Lua layer SHALL NOT duplicate the verb logic.

#### Scenario: adding a new ctl subcommand surfaces a new slash command for free

- **WHEN** a developer adds a new `milliwaysctl foo` subcommand
- **THEN** typing `/foo` in any milliways-term tab dispatches to it without changing the wezterm Lua code

### Requirement: User documentation

The `cmd/milliwaysctl/README.wezterm.md` and project `README.md` SHALL document the slash-command dispatcher and the `milliwaysctl local` verb tree, including default keybindings and how to override them.

#### Scenario: README explains the bootstrap flow

- **WHEN** a new user reads README.md's local-models section
- **THEN** they see the `/local-install-server` → `/local-setup-model <name>` flow as the primary path
- **AND** the manual `scripts/install_local.sh` invocation is documented as a fallback only

# Tasks — milliways-login

## 1. maitre/onboard.go — Auth Infrastructure

- [x] 1.1 Add `isTTY() bool` helper using `golang.org/x/term` to check if stdin is a terminal
- [x] 1.2 Add `UpdateKitchenAuth(kitchen, apiKey string) error` — reads carte.yaml, patches `kitchens.<name>.http_client.auth_key`, writes atomically with `.bak` backup
- [x] 1.3 Add `LoginKitchen(kitchen string) error` — dispatches to correct auth method based on kitchen type, returns descriptive error on failure
- [x] 1.4 Add `loginCLIOAuth(name, cli, authCmd string) error` — for claude/gemini, runs `exec.Command(cli, "auth", "login")`
- [x] 1.5 Add `loginInteractiveTUI(name, cli string, args ...string) error` — for opencode, runs `exec.Command(cli, args...)`
- [x] 1.6 Add `loginAPIKey(name string) error` — prompts for masked API key (skip if !isTTY), calls UpdateKitchenAuth
- [x] 1.7 Add `loginEnvVar(name, envVar, docsURL string) error` — prints env var setup instructions
- [x] 1.8 Add `loginOllama() error` — checks localhost:11434 reachability, prints status

## 2. CLI — `milliways login` Subcommand

- [x] 2.1 Add `loginCmd` as a subcommand on rootCmd in `cmd/milliways/main.go`
- [x] 2.2 `login --list` flag: show all kitchens with name, auth status, and login action
- [x] 2.3 `login <kitchen>`: call `maitre.LoginKitchen(kitchen)` and print result
- [x] 2.4 `login` with no args: print usage ("usage: milliways login <kitchen>")
- [x] 2.5 Run `go fmt`, `go vet`, `go test ./cmd/milliways/...` after changes

## 3. TUI — `/login` Command

- [x] 3.1 Add `case "login":` in `executePaletteCommand` in `internal/tui/app.go`
- [x] 3.2 `/login` with no kitchen arg: render kitchen auth status list using `maitre.Diagnose` + auth type info, show as formatted text via `appendCommandFeedback`
- [x] 3.3 `/login <kitchen>`: call `maitre.LoginKitchen(kitchen)`, print result to command feedback
- [x] 3.4 MiniMax login: use TTY masked input (`term.ReadPassword`) for API key prompt
- [x] 3.5 Non-TTY login: print env var instructions instead of blocking on prompt
- [x] 3.6 Run `go fmt`, `go vet`, `go test ./internal/tui/...` after changes

## 4. Tests

- [x] 4.1 Unit test: `UpdateKitchenAuth` patches correct kitchen, creates `.bak`, preserves other YAML fields
- [x] 4.2 Unit test: `LoginKitchen` dispatches to correct method for each kitchen type
- [x] 4.3 Unit test: `loginEnvVar` formats env var name and docs URL correctly
- [x] 4.4 Unit test: `loginOllama` reports reachable vs unreachable
- [x] 4.5 Integration test: `milliways login groq` prints env var instructions without error

## 5. Documentation

- [x] 5.1 Add `milliways login <kitchen>` to CLI help text summary in rootCmd Long description
- [x] 5.2 Add `/login` to the TUI slash command list (check if there's a help/command reference)

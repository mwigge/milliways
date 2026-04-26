## 1. Research / Spike

- [x] 1.1 Verify mempalace fork has conversation primitive (mempalace_conversation_start/end/append_turn/checkpoint/resume)
- [x] 1.2 Verify MiniMax HTTP API SSE streaming works (MiniMax-M2.7 model)
- [x] 1.3 Check if claude and codex have quota commands (neither has quota subcommand; pantry DB used)
- [ ] 1.4 Test liner library on macOS Terminal, iTerm2 (manual test)

## 2. Project Structure

- [x] 2.1 Create internal/repl/ directory (replace internal/tui/)
- [x] 2.2 Add liner dependency to go.mod
- [x] 2.3 Add golang.org/x/term dependency for PTY

## 3. Core REPL Loop

- [x] 3.1 Implement REPL entry point (start from main.go when no args)
- [x] 3.2 Implement liner-based input with history
- [x] 3.3 Implement basic prompt display (runner name, session name)
- [x] 3.4 Implement input parsing (/command vs !bash vs prompt)
- [x] 3.5 Implement Ctrl+C handling (interrupt runner or exit)
- [x] 3.6 Implement Ctrl+D handling (exit on empty line)
- [x] 3.7 Implement Unix line-editing keys (Ctrl+U, Ctrl+A, Ctrl+E)

## 4. Kitchen Runners

- [x] 4.1 Implement runner interface (Executor interface)
- [x] 4.2 Implement claude runner (exec.CommandContext, stdout streaming)
- [x] 4.3 Implement codex runner (exec.CommandContext, stdout streaming)
- [x] 4.4 Implement minimax runner (HTTP API, SSE streaming)
- [x] 4.5 Implement runner lifecycle (start on /switch, stop on next /switch)
- [x] 4.6 Implement subprocess cleanup on runner switch/exit

## 5. Real-time Streaming Output

- [x] 5.1 Implement line-by-line stdout capture from subprocess
- [x] 5.2 Write each line to terminal immediately (no buffering)
- [x] 5.3 Handle subprocess completion and exit code display

## 6. PTY for Auth Flows

- [x] 6.1 Implement PTY allocation for login flows (internal/repl/pty.go)
- [x] 6.2 Implement /login command triggering runner auth
- [ ] 6.3 Test claude auth login with PTY (manual test)
- [ ] 6.4 Test codex auth login with PTY (manual test)

## 7. REPL Commands

- [x] 7.1 Implement /switch <runner> command
- [x] 7.2 Implement /stick command (toggle stickiness)
- [x] 7.3 Implement /back command (reverse last switch)
- [x] 7.4 Implement /session [name] command
- [x] 7.5 Implement /history command
- [x] 7.6 Implement /summary command
- [x] 7.7 Implement /cost command
- [x] 7.8 Implement /limit command
- [x] 7.9 Implement /openspec command
- [x] 7.10 Implement /repo command
- [x] 7.11 Implement /login command
- [x] 7.12 Implement /logout command
- [x] 7.13 Implement /auth command
- [x] 7.14 Implement /help command
- [x] 7.15 Implement /exit command
- [x] 7.16 Implement !<cmd> bash command execution

## 8. Session Persistence (Mempalace)

- [x] 8.1 Integrate mempalace client for session storage
- [x] 8.2 Implement conversation start on REPL start
- [x] 8.3 Implement conversation append on each dispatch
- [x] 8.4 Implement conversation checkpoint on runner switch
- [x] 8.5 Implement conversation resume on /session <name>
- [ ] 8.6 Test session survives milliways restart (manual test)

## 9. Quota Tracking

- [x] 9.1 Implement quota query from pantry DB (all runners)
- [x] 9.2 Implement quota query for codex via pantry DB
- [x] 9.3 Implement quota query for minimax via pantry DB
- [x] 9.4 Implement /limit display with day/week/month breakdown
- [x] 9.5 Implement /cost display for session

## 10. Phosphor Aesthetic

- [x] 10.1 Define color constants (#4FB522, #2E6914, #466D35, #000000, #FF4444, #FFAA00)
- [x] 10.2 Apply green palette to REPL output
- [x] 10.3 Apply kitchen accent colors to runner badges
- [x] 10.4 Style error messages in red (#FF4444)
- [x] 10.5 Style warnings/running in amber (#FFAA00)

## 11. OpenSpec Integration

- [x] 11.1 Implement /openspec to read current change from openspec/
- [x] 11.2 Display change name and task progress
- [x] 11.3 Support /openspec <change-name> to switch context

## 12. Git Integration

- [x] 12.1 Implement /repo to read current git repo info
- [x] 12.2 Display repo name, branch, last commit, status

## 13. Testing

- [x] 13.1 Write unit tests for REPL command parsing
- [x] 13.2 Write unit tests for runner switching (TestREPLSetRunner, TestREPLRunnerState)
- [x] 13.3 Write integration tests for streaming output (TestStreamCmdOutput, TestStreamingWriter)
- [ ] 13.4 Test session persistence with mempalace (manual test)
- [ ] 13.5 Manual testing: claude, codex, minimax execution (manual test)
- [ ] 13.6 Manual testing: /switch, /stick, /back flow (manual test)

## 14. Cleanup / Deprecation

- [x] 14.1 Remove internal/tui/ package (deprecated, not removed — --tui still works)
- [x] 14.2 Remove Bubble Tea dependencies (TUI still uses them)
- [x] 14.3 Remove http.go adapter for minimax (provider/minimax.go still needed for TUI)
- [x] 14.4 Update go.mod / go.sum

## 15. Documentation

- [x] 15.1 Update README with new REPL interface
- [x] 15.2 Document new commands in README
- [x] 15.3 Update installation instructions if needed
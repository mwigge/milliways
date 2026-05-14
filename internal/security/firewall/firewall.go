// Copyright 2024 The milliways Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package firewall classifies shell commands before execution. It is a
// deterministic policy layer; it does not execute commands and does not depend
// on model judgement.
package firewall

import (
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/mwigge/milliways/internal/security"
)

// Decision is the action the command firewall recommends for a command.
type Decision string

const (
	DecisionAllow             Decision = "allow"
	DecisionWarn              Decision = "warn"
	DecisionBlock             Decision = "block"
	DecisionNeedsConfirmation Decision = "needs-confirmation"
)

// RiskCategory identifies the primitive that made a command risky.
type RiskCategory string

const (
	RiskPackageInstall  RiskCategory = "package-install"
	RiskPersistence     RiskCategory = "persistence"
	RiskExfiltration    RiskCategory = "exfiltration"
	RiskSecretRead      RiskCategory = "secret-read"
	RiskNetworkDownload RiskCategory = "network-download"
	RiskShellEval       RiskCategory = "shell-eval"
	RiskComplexUnparsed RiskCategory = "complex-unparsed"
	RiskIOC             RiskCategory = "ioc"
)

// Risk is one classified command risk.
type Risk struct {
	Category RiskCategory
	Reason   string
	Evidence string
}

// Policy controls how classified risks map to a decision.
type Policy struct {
	Mode                       security.Mode
	SafePackageInstalls        bool
	PersistenceApproved        bool
	AllowedNetworkDownloads    bool
	AllowedSecretReads         bool
	AllowedShellEval           bool
	BlockNetworkDownloadsInCI  bool
	SuspiciousNetworkArtifacts []string
}

// Request is the command firewall input.
type Request struct {
	Command  string
	RunnerID string
	CWD      string
	Policy   Policy
	Posture  security.Posture
}

// Result is the command firewall output.
type Result struct {
	Decision Decision
	Risks    []Risk
	Mode     security.Mode
	Parsed   bool
	Reason   string
}

// Evaluate classifies and applies policy to a command.
func Evaluate(req Request) Result {
	mode := security.NormalizeMode(req.Policy.Mode)
	if mode == security.ModeOff {
		risks, parsed := Classify(req.Command, req.Policy)
		return Result{Decision: DecisionAllow, Risks: risks, Mode: mode, Parsed: parsed, Reason: "security mode off"}
	}

	risks, parsed := Classify(req.Command, req.Policy)
	decision, reason := decide(mode, req.Policy, risks)
	return Result{Decision: decision, Risks: risks, Mode: mode, Parsed: parsed, Reason: reason}
}

// Classify returns deterministic risk primitives present in command.
func Classify(command string, policy Policy) ([]Risk, bool) {
	parsed := true
	segments, parseOK := splitShell(command)
	if !parseOK {
		parsed = false
		segments = []string{command}
	}

	seen := make(map[RiskCategory]Risk)
	add := func(category RiskCategory, reason, evidence string) {
		if _, ok := seen[category]; ok {
			return
		}
		seen[category] = Risk{Category: category, Reason: reason, Evidence: evidence}
	}

	lowerCommand := strings.ToLower(command)
	for _, indicator := range allIOCIndicators(policy) {
		if indicator != "" && strings.Contains(lowerCommand, strings.ToLower(indicator)) {
			add(RiskIOC, "command references a suspicious domain, IP, or IOC", indicator)
			break
		}
	}

	if hasShellEval(command) {
		add(RiskShellEval, "command uses dynamic shell evaluation", firstShellEvalEvidence(command))
	}

	tokensBySegment := make([][]string, 0, len(segments))
	for _, segment := range segments {
		tokens, ok := shellFields(segment)
		if !ok {
			parsed = false
		}
		tokensBySegment = append(tokensBySegment, tokens)
		classifyTokens(tokens, add)
	}

	if isComplex(command, segments, parsed) && hasRiskPrimitive(command) {
		add(RiskComplexUnparsed, "command is too complex to classify safely and contains risky primitives", complexEvidence(command))
	}
	if looksLikeExfiltration(command, tokensBySegment) {
		add(RiskExfiltration, "command combines local data collection or secrets with network transfer", exfilEvidence(command))
	}

	out := make([]Risk, 0, len(seen))
	for _, risk := range seen {
		out = append(out, risk)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Category < out[j].Category
	})
	return out, parsed
}

func decide(mode security.Mode, policy Policy, risks []Risk) (Decision, string) {
	if len(risks) == 0 {
		return DecisionAllow, "no risky command primitives detected"
	}
	if mode == security.ModeObserve {
		return DecisionAllow, "security mode observe records risks without blocking"
	}

	has := riskSet(risks)
	if has[RiskIOC] || has[RiskExfiltration] || has[RiskComplexUnparsed] {
		return DecisionBlock, "command contains a blocking security primitive"
	}
	if has[RiskSecretRead] && !policy.AllowedSecretReads {
		if mode == security.ModeCI || has[RiskNetworkDownload] {
			return DecisionBlock, "command may expose local secrets"
		}
		return DecisionNeedsConfirmation, "command reads known secret paths"
	}
	if has[RiskPersistence] && !policy.PersistenceApproved {
		if mode == security.ModeStrict || mode == security.ModeCI {
			return DecisionBlock, "user-level persistence changes require explicit approval"
		}
		return DecisionWarn, "command changes user-level persistence"
	}
	if has[RiskPackageInstall] && !policy.SafePackageInstalls {
		if mode == security.ModeStrict || mode == security.ModeCI {
			return DecisionBlock, "package install commands require safe package policy"
		}
		return DecisionWarn, "command changes dependencies"
	}
	if has[RiskShellEval] && !policy.AllowedShellEval {
		if mode == security.ModeCI {
			return DecisionBlock, "dynamic shell evaluation is not allowed in ci mode"
		}
		if mode == security.ModeStrict {
			return DecisionNeedsConfirmation, "dynamic shell evaluation requires confirmation"
		}
		return DecisionWarn, "command uses dynamic shell evaluation"
	}
	if has[RiskNetworkDownload] && !policy.AllowedNetworkDownloads {
		if mode == security.ModeCI && policy.BlockNetworkDownloadsInCI {
			return DecisionBlock, "network downloads are blocked in ci mode"
		}
		if mode == security.ModeStrict {
			return DecisionNeedsConfirmation, "network download requires confirmation"
		}
		return DecisionWarn, "command downloads from the network"
	}

	return DecisionAllow, "risky primitives are allowed by policy"
}

func classifyTokens(tokens []string, add func(RiskCategory, string, string)) {
	if len(tokens) == 0 {
		return
	}
	cmd := base(tokens[0])
	switch {
	case isPackageInstall(tokens):
		add(RiskPackageInstall, "command invokes a package manager install/update operation", strings.Join(head(tokens, 4), " "))
	case isNetworkDownload(tokens):
		add(RiskNetworkDownload, "command fetches content from the network", strings.Join(head(tokens, 3), " "))
	case isPersistenceCommand(tokens):
		add(RiskPersistence, "command creates or enables user-level persistence", strings.Join(head(tokens, 5), " "))
	case isSecretRead(tokens):
		add(RiskSecretRead, "command reads known secret paths", firstSecretEvidence(tokens))
	case cmd == "eval":
		add(RiskShellEval, "command uses eval", "eval")
	}
}

func isPackageInstall(tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}
	cmd := base(tokens[0])
	args := tokens[1:]
	switch cmd {
	case "npm", "pnpm":
		return hasSubcommand(args, "install", "i", "add", "update", "upgrade", "ci")
	case "yarn":
		return hasSubcommand(args, "add", "install", "upgrade", "up")
	case "bun":
		return hasSubcommand(args, "add", "install", "update")
	case "pip", "pip3":
		return hasSubcommand(args, "install")
	case "uv":
		return hasSubsequence(args, "pip", "install") || hasSubcommand(args, "add", "sync")
	case "poetry":
		return hasSubcommand(args, "add", "install", "update")
	case "go":
		return hasSubcommand(args, "install", "get")
	case "cargo":
		return hasSubcommand(args, "install", "add", "update")
	}
	return false
}

func isNetworkDownload(tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}
	cmd := base(tokens[0])
	if cmd == "curl" || cmd == "wget" || cmd == "fetch" || cmd == "http" || cmd == "https" {
		return hasURL(tokens)
	}
	return false
}

func isPersistenceCommand(tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}
	cmd := base(tokens[0])
	if cmd == "systemctl" && hasSubcommand(tokens[1:], "enable", "link") {
		return true
	}
	if cmd == "launchctl" && hasSubcommand(tokens[1:], "load", "bootstrap", "enable") {
		return true
	}
	if cmd == "crontab" {
		return true
	}
	for _, tok := range tokens {
		t := strings.ToLower(expandHome(tok))
		if strings.Contains(t, "/.config/systemd/user/") ||
			strings.Contains(t, "/etc/systemd/") ||
			strings.Contains(t, "/library/launchagents/") ||
			strings.Contains(t, "/launchdaemons/") ||
			strings.HasSuffix(t, "/.bashrc") ||
			strings.HasSuffix(t, "/.zshrc") ||
			strings.HasSuffix(t, "/.profile") ||
			strings.HasSuffix(t, "/.config/fish/config.fish") {
			return true
		}
	}
	return false
}

func isSecretRead(tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}
	cmd := base(tokens[0])
	readers := map[string]bool{
		"cat": true, "less": true, "more": true, "head": true, "tail": true,
		"grep": true, "rg": true, "awk": true, "sed": true, "strings": true,
		"base64": true, "xxd": true, "openssl": true, "tar": true, "zip": true,
	}
	if !readers[cmd] && !hasNetworkUploader(tokens) {
		return false
	}
	return firstSecretEvidence(tokens) != ""
}

func looksLikeExfiltration(command string, tokensBySegment [][]string) bool {
	lower := strings.ToLower(command)
	hasNetwork := strings.Contains(lower, "http://") || strings.Contains(lower, "https://")
	hasUpload := strings.Contains(lower, "--upload-file") || strings.Contains(lower, " -t ") ||
		strings.Contains(lower, " -d ") || strings.Contains(lower, "--data") ||
		strings.Contains(lower, " --form") || strings.Contains(lower, " -f ")
	hasArchive := strings.Contains(lower, "tar ") || strings.Contains(lower, " zip ") ||
		strings.Contains(lower, "gzip ") || strings.Contains(lower, "base64 ")
	hasDiscovery := strings.Contains(lower, " find ") || strings.Contains(lower, "grep ") ||
		strings.Contains(lower, " rg ") || strings.Contains(lower, "env") ||
		strings.Contains(lower, "printenv")
	hasPipeToNetwork := strings.Contains(command, "|") && hasNetwork

	for _, tokens := range tokensBySegment {
		if hasNetworkUploader(tokens) && (firstSecretEvidence(tokens) != "" || hasArchive || hasDiscovery || hasPipeToNetwork) {
			return true
		}
		if base(firstToken(tokens)) == "scp" || base(firstToken(tokens)) == "rsync" || base(firstToken(tokens)) == "nc" || base(firstToken(tokens)) == "netcat" {
			if firstSecretEvidence(tokens) != "" || hasArchive || hasDiscovery {
				return true
			}
		}
	}
	return hasNetwork && (hasUpload || hasPipeToNetwork) && (hasArchive || hasDiscovery || containsSecretPath(lower))
}

func hasNetworkUploader(tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}
	cmd := base(tokens[0])
	if cmd == "curl" || cmd == "wget" || cmd == "http" || cmd == "https" {
		for _, tok := range tokens[1:] {
			t := strings.ToLower(tok)
			if t == "-d" || t == "--data" || strings.HasPrefix(t, "--data=") ||
				t == "--data-binary" || strings.HasPrefix(t, "--data-binary=") ||
				t == "-f" || t == "--form" || strings.HasPrefix(t, "--form=") ||
				t == "-t" || t == "--upload-file" || strings.HasPrefix(t, "--upload-file=") {
				return true
			}
		}
	}
	return cmd == "scp" || cmd == "rsync" || cmd == "nc" || cmd == "netcat"
}

func hasShellEval(command string) bool {
	lower := strings.ToLower(command)
	if strings.Contains(command, "`") || strings.Contains(command, "$(") || strings.Contains(command, "<(") || strings.Contains(command, ">(") {
		return true
	}
	if strings.Contains(lower, "| sh") || strings.Contains(lower, "|sh") ||
		strings.Contains(lower, "| bash") || strings.Contains(lower, "|bash") ||
		strings.Contains(lower, "| zsh") || strings.Contains(lower, "|zsh") {
		return true
	}
	for _, needle := range []string{" eval ", "bash -c", "sh -c", "zsh -c", "fish -c", "python -c", "python3 -c", "node -e", "perl -e", "ruby -e"} {
		if strings.Contains(" "+lower, needle) {
			return true
		}
	}
	return false
}

func splitShell(command string) ([]string, bool) {
	var out []string
	var b strings.Builder
	var quote rune
	escaped := false
	depth := 0
	ok := true
	skipNext := false
	flush := func() {
		part := strings.TrimSpace(b.String())
		if part != "" {
			out = append(out, part)
		}
		b.Reset()
	}
	for i, r := range command {
		if skipNext {
			skipNext = false
			continue
		}
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			b.WriteRune(r)
			escaped = true
			continue
		}
		if quote != 0 {
			b.WriteRune(r)
			if r == quote {
				quote = 0
			}
			continue
		}
		switch r {
		case '\'', '"':
			quote = r
			b.WriteRune(r)
		case '(':
			depth++
			b.WriteRune(r)
		case ')':
			if depth == 0 {
				ok = false
			} else {
				depth--
			}
			b.WriteRune(r)
		case ';', '\n':
			flush()
		case '&', '|':
			if i+1 < len(command) && rune(command[i+1]) == r {
				flush()
				skipNext = true
				continue
			}
			if r == '|' {
				flush()
				continue
			}
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	if quote != 0 || depth != 0 {
		ok = false
	}
	flush()
	if len(out) == 0 && strings.TrimSpace(command) != "" {
		out = append(out, strings.TrimSpace(command))
	}
	return out, ok
}

func shellFields(s string) ([]string, bool) {
	var fields []string
	var b strings.Builder
	var quote rune
	escaped := false
	ok := true
	flush := func() {
		if b.Len() > 0 {
			fields = append(fields, b.String())
			b.Reset()
		}
	}
	for _, r := range s {
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
				continue
			}
			b.WriteRune(r)
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		if unicode.IsSpace(r) {
			flush()
			continue
		}
		b.WriteRune(r)
	}
	if quote != 0 || escaped {
		ok = false
	}
	flush()
	return fields, ok
}

func isComplex(command string, segments []string, parsed bool) bool {
	if !parsed {
		return true
	}
	if len(command) > 800 && len(segments) > 4 {
		return true
	}
	if len(segments) > 10 {
		return true
	}
	return strings.Count(command, "|") >= 4 || strings.Count(command, ";") >= 6
}

func hasRiskPrimitive(command string) bool {
	lower := strings.ToLower(command)
	for _, primitive := range []string{
		"curl", "wget", "http://", "https://", "npm ", "pnpm ", "yarn ", "bun ",
		"pip ", "pip3 ", "uv ", "poetry ", "go install", "cargo install",
		"systemctl", "launchctl", "crontab", ".ssh", ".env", ".npmrc", ".pypirc",
		".aws/credentials", "eval", "$(", "`",
	} {
		if strings.Contains(lower, primitive) {
			return true
		}
	}
	return false
}

func hasURL(tokens []string) bool {
	for _, tok := range tokens {
		t := strings.ToLower(tok)
		if strings.HasPrefix(t, "http://") || strings.HasPrefix(t, "https://") ||
			strings.Contains(t, "=http://") || strings.Contains(t, "=https://") {
			return true
		}
	}
	return false
}

func firstSecretEvidence(tokens []string) string {
	for _, tok := range tokens {
		if isSecretPath(tok) {
			return tok
		}
	}
	return ""
}

func isSecretPath(tok string) bool {
	return containsSecretPath(strings.ToLower(expandHome(strings.Trim(tok, `"'`))))
}

func containsSecretPath(s string) bool {
	secretNeedles := []string{
		"/.ssh/id_rsa", "/.ssh/id_ed25519", "/.ssh/config", "/.ssh/known_hosts",
		"/.aws/credentials", "/.aws/config", "/.config/gh/hosts.yml",
		"/.config/gcloud/application_default_credentials.json",
		"/.docker/config.json", "/.kube/config", "/.netrc", "/.npmrc", "/.pypirc",
		"/.cargo/credentials", "/.gem/credentials", "/.azure/accesstokens.json",
	}
	for _, needle := range secretNeedles {
		if strings.Contains(s, needle) {
			return true
		}
	}
	baseName := filepath.Base(s)
	return baseName == ".env" || strings.HasPrefix(baseName, ".env.") ||
		strings.Contains(baseName, "token") || strings.Contains(baseName, "secret")
}

func allIOCIndicators(policy Policy) []string {
	out := []string{"git-tanstack.com", "getsession.org", "83.142.209.194", "gh-token-monitor"}
	out = append(out, policy.SuspiciousNetworkArtifacts...)
	return out
}

func hasSubcommand(args []string, names ...string) bool {
	want := make(map[string]bool, len(names))
	for _, name := range names {
		want[name] = true
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		return want[strings.ToLower(arg)]
	}
	return false
}

func hasSubsequence(args []string, first, second string) bool {
	seenFirst := false
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		arg = strings.ToLower(arg)
		if !seenFirst {
			seenFirst = arg == first
			continue
		}
		return arg == second
	}
	return false
}

func firstShellEvalEvidence(command string) string {
	lower := strings.ToLower(command)
	for _, ev := range []string{"$(", "`", "<(", ">(", "eval", "bash -c", "sh -c", "zsh -c", "python -c", "node -e"} {
		if strings.Contains(lower, ev) {
			return ev
		}
	}
	return "shell-eval"
}

func exfilEvidence(command string) string {
	if len(command) > 120 {
		return command[:120]
	}
	return command
}

func complexEvidence(command string) string {
	if len(command) > 120 {
		return command[:120]
	}
	return command
}

func expandHome(s string) string {
	if strings.HasPrefix(s, "~/") {
		return "/home/user/" + strings.TrimPrefix(s, "~/")
	}
	return s
}

func firstToken(tokens []string) string {
	if len(tokens) == 0 {
		return ""
	}
	return tokens[0]
}

func base(s string) string {
	return filepath.Base(strings.ToLower(strings.Trim(s, `"'`)))
}

func head(tokens []string, n int) []string {
	if len(tokens) <= n {
		return tokens
	}
	return tokens[:n]
}

func riskSet(risks []Risk) map[RiskCategory]bool {
	out := make(map[RiskCategory]bool, len(risks))
	for _, risk := range risks {
		out[risk.Category] = true
	}
	return out
}

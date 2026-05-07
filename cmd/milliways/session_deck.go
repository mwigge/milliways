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

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	deckStatusIdle      = "idle"
	deckStatusThinking  = "thinking"
	deckStatusStreaming = "streaming"
	deckStatusRunning   = "running"
	deckStatusError     = "error"
)

type sessionDeck struct {
	mu     sync.RWMutex
	order  []string
	states map[string]*sessionDeckState
	active string
}

type sessionDeckState struct {
	Provider    string
	Model       string
	ModelSource string
	Handle      int64
	Status      string
	PromptCount int
	TurnCount   int
	Unread      int
	Queue       int
	Retries     int

	InputTokens  int
	OutputTokens int
	TotalTokens  int
	CostUSD      float64
	CurrentTrace string
	LastTrace    string
	LatencyMS    float64
	TTFTMS       float64
	TokenRate    float64
	ErrorCount   int

	LastThinking string
	LastError    string
	LastPrompt   string
	LastSaved    bool
	LastStarted  time.Time
	LastUpdated  time.Time
	LastDone     time.Time

	Buffer []sessionDeckBlock
}

type sessionDeckBlock struct {
	Kind string
	Text string
	At   time.Time
}

type sessionDeckSnapshot struct {
	Active string
	States []sessionDeckState
}

type daemonDeckSnapshot struct {
	Active   string                    `json:"active"`
	Sessions []daemonDeckSessionStatus `json:"sessions"`
}

type daemonDeckSessionStatus struct {
	AgentID      string            `json:"agent_id"`
	Handle       int64             `json:"handle"`
	Status       string            `json:"status"`
	PromptCount  int               `json:"prompt_count"`
	TurnCount    int               `json:"turn_count"`
	InputTokens  int               `json:"input_tokens"`
	OutputTokens int               `json:"output_tokens"`
	TotalTokens  int               `json:"total_tokens"`
	CostUSD      float64           `json:"cost_usd"`
	CurrentTrace string            `json:"current_trace"`
	LastTrace    string            `json:"last_trace"`
	LatencyMS    float64           `json:"latency_ms"`
	TTFTMS       float64           `json:"ttft_ms"`
	TokenRate    float64           `json:"token_rate"`
	ErrorCount   int               `json:"error_count"`
	QueueDepth   int               `json:"queue_depth"`
	Model        string            `json:"model"`
	ModelSource  string            `json:"model_source"`
	LastThinking string            `json:"last_thinking"`
	LastError    string            `json:"last_error"`
	LastPrompt   string            `json:"last_prompt"`
	LastUpdated  time.Time         `json:"last_updated"`
	Buffer       []daemonDeckBlock `json:"buffer"`
}

type daemonDeckBlock struct {
	Kind string    `json:"kind"`
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

func newSessionDeck(order []string) *sessionDeck {
	d := &sessionDeck{
		order:  append([]string(nil), order...),
		states: make(map[string]*sessionDeckState, len(order)),
	}
	for _, provider := range order {
		d.stateLocked(provider)
	}
	return d
}

func (d *sessionDeck) ApplyDaemonSnapshot(s daemonDeckSnapshot) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if s.Active != "" {
		d.active = s.Active
	}
	for _, src := range s.Sessions {
		if src.AgentID == "" {
			continue
		}
		st := d.stateLocked(src.AgentID)
		st.Handle = src.Handle
		st.Status = fallbackStatus(src.Status)
		if strings.TrimSpace(src.Model) != "" {
			st.Model = src.Model
		}
		st.ModelSource = src.ModelSource
		st.PromptCount = src.PromptCount
		st.TurnCount = src.TurnCount
		st.InputTokens = src.InputTokens
		st.OutputTokens = src.OutputTokens
		st.TotalTokens = src.TotalTokens
		st.CostUSD = src.CostUSD
		st.CurrentTrace = src.CurrentTrace
		st.LastTrace = src.LastTrace
		st.LatencyMS = src.LatencyMS
		st.TTFTMS = src.TTFTMS
		st.TokenRate = src.TokenRate
		st.ErrorCount = src.ErrorCount
		st.Queue = src.QueueDepth
		st.LastThinking = src.LastThinking
		st.LastError = src.LastError
		st.LastPrompt = src.LastPrompt
		st.LastUpdated = src.LastUpdated
		if len(src.Buffer) > 0 {
			st.Buffer = st.Buffer[:0]
			for _, block := range src.Buffer {
				st.Buffer = append(st.Buffer, sessionDeckBlock{
					Kind: block.Kind,
					Text: block.Text,
					At:   block.At,
				})
			}
		}
	}
}

func (d *sessionDeck) stateLocked(provider string) *sessionDeckState {
	if d.states == nil {
		d.states = make(map[string]*sessionDeckState)
	}
	if st := d.states[provider]; st != nil {
		return st
	}
	st := &sessionDeckState{
		Provider: provider,
		Model:    runnerModelSpec(provider).current,
		Status:   deckStatusIdle,
	}
	d.states[provider] = st
	if !containsString(d.order, provider) {
		d.order = append(d.order, provider)
	}
	return st
}

func fallbackStatus(s string) string {
	if strings.TrimSpace(s) == "" {
		return deckStatusIdle
	}
	return s
}

func (d *sessionDeck) SetActive(provider string) {
	if d == nil || provider == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	st := d.stateLocked(provider)
	st.Unread = 0
	if st.Model == "" {
		st.Model = runnerModelSpec(provider).current
	}
	d.active = provider
}

func (d *sessionDeck) BindSession(provider string, handle int64) {
	if d == nil || provider == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	st := d.stateLocked(provider)
	st.Handle = handle
	if st.Model == "" {
		st.Model = runnerModelSpec(provider).current
	}
	st.Status = deckStatusIdle
	st.LastUpdated = time.Now()
}

func (d *sessionDeck) MarkPrompt(provider, prompt string) {
	if d == nil || provider == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	st := d.stateLocked(provider)
	st.Status = deckStatusThinking
	st.LastPrompt = prompt
	st.LastError = ""
	st.LastSaved = false
	st.LastStarted = time.Now()
	st.LastUpdated = st.LastStarted
	st.PromptCount++
	st.appendBlock("prompt", prompt)
}

func (d *sessionDeck) MarkThinking(provider, text string) {
	if d == nil || provider == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	st := d.stateLocked(provider)
	st.Status = deckStatusThinking
	st.LastThinking = text
	st.LastUpdated = time.Now()
	st.appendBlock("thinking", text)
	if provider != d.active {
		st.Unread++
	}
}

func (d *sessionDeck) AppendData(provider, text string, visible bool) {
	if d == nil || provider == "" || text == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	st := d.stateLocked(provider)
	st.Status = deckStatusStreaming
	st.LastUpdated = time.Now()
	st.appendBlock("response", text)
	if provider != d.active && !visible {
		st.Unread++
	}
}

func (d *sessionDeck) MarkChunkEnd(provider string, inTok, outTok int, cost float64, saved bool) {
	if d == nil || provider == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	st := d.stateLocked(provider)
	st.Status = deckStatusIdle
	st.InputTokens += inTok
	st.OutputTokens += outTok
	st.TotalTokens += inTok + outTok
	st.CostUSD += cost
	st.LastSaved = saved
	st.LastDone = time.Now()
	st.LastUpdated = st.LastDone
	st.TurnCount++
}

func (d *sessionDeck) MarkError(provider, msg string) {
	if d == nil || provider == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	st := d.stateLocked(provider)
	st.Status = deckStatusError
	st.LastError = msg
	st.LastUpdated = time.Now()
	if provider != d.active {
		st.Unread++
	}
	st.appendBlock("error", msg)
}

func (d *sessionDeck) MarkParallelDispatch(providers []string, prompt string) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now()
	for _, provider := range providers {
		st := d.stateLocked(provider)
		st.Status = deckStatusRunning
		st.LastPrompt = prompt
		st.LastStarted = now
		st.LastUpdated = now
		st.Queue++
		st.appendBlock("parallel", prompt)
		if provider != d.active {
			st.Unread++
		}
	}
}

func (d *sessionDeck) MarkParallelSlots(slots []parallelSlotSummary) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now()
	for _, slot := range slots {
		st := d.stateLocked(slot.Provider)
		st.Handle = slot.Handle
		st.Status = deckStatusRunning
		st.LastUpdated = now
	}
}

type parallelSlotSummary struct {
	Provider string
	Handle   int64
}

func (d *sessionDeck) Next(delta int) string {
	if d == nil {
		return ""
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.order) == 0 {
		return ""
	}
	idx := 0
	for i, provider := range d.order {
		if provider == d.active {
			idx = i
			break
		}
	}
	idx = (idx + delta) % len(d.order)
	if idx < 0 {
		idx += len(d.order)
	}
	d.active = d.order[idx]
	d.stateLocked(d.active).Unread = 0
	return d.active
}

func (d *sessionDeck) Snapshot() sessionDeckSnapshot {
	if d == nil {
		return sessionDeckSnapshot{}
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	snap := sessionDeckSnapshot{Active: d.active}
	for _, provider := range d.order {
		if st := d.states[provider]; st != nil {
			cp := *st
			cp.Buffer = append([]sessionDeckBlock(nil), st.Buffer...)
			snap.States = append(snap.States, cp)
		}
	}
	return snap
}

func (d *sessionDeck) ActiveBuffer() (string, []sessionDeckBlock) {
	if d == nil {
		return "", nil
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	st := d.states[d.active]
	if st == nil {
		return d.active, nil
	}
	return d.active, append([]sessionDeckBlock(nil), st.Buffer...)
}

func (st *sessionDeckState) appendBlock(kind, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	st.Buffer = append(st.Buffer, sessionDeckBlock{Kind: kind, Text: text, At: time.Now()})
	const maxBlocks = 80
	if over := len(st.Buffer) - maxBlocks; over > 0 {
		st.Buffer = st.Buffer[over:]
	}
}

func renderSessionStatusPanel(s sessionDeckSnapshot, width int) string {
	if width <= 0 {
		width = 80
	}
	active := s.Active
	if active == "" {
		active = "none"
	}
	var current sessionDeckState
	for _, st := range s.States {
		if st.Provider == s.Active {
			current = st
			break
		}
	}
	cwd, _ := os.Getwd()
	project := filepath.Base(cwd)
	if project == "." || project == "/" || project == "" {
		project = cwd
	}
	parts := []string{
		"session " + active,
		"model " + modelLabel(current.Model, current.ModelSource),
		"status " + fallbackDash(current.Status),
		"project " + fallbackDash(project),
	}
	if current.Handle > 0 {
		parts = append(parts, fmt.Sprintf("handle %d", current.Handle))
	}
	if usage := formatUsageCompact(usageStats{TotalTokens: current.TotalTokens, CostUSD: current.CostUSD}); usage != "" {
		parts = append(parts, usage)
	}
	if current.LastSaved {
		parts = append(parts, "saved")
	}
	out := strings.Join(parts, "  |  ")
	if len(out) > width {
		return out[:width-1] + "…"
	}
	return out
}

func modelLabel(model, source string) string {
	model = fallbackDash(strings.TrimSpace(model))
	source = strings.TrimSpace(source)
	if source == "" {
		return model
	}
	return model + " (" + source + ")"
}

func renderObservabilityPanel(s sessionDeckSnapshot, width int) string {
	if width <= 0 {
		width = 80
	}
	var rows []string
	for _, st := range s.States {
		if st.Handle == 0 && st.PromptCount == 0 && st.Unread == 0 && st.Status == deckStatusIdle {
			continue
		}
		row := fmt.Sprintf("%-8s %-9s", st.Provider, st.Status)
		if st.Unread > 0 {
			row += fmt.Sprintf(" unread:%d", st.Unread)
		}
		if usage := formatUsageCompact(usageStats{InputTokens: st.InputTokens, OutputTokens: st.OutputTokens, CostUSD: st.CostUSD}); usage != "" {
			row += " usage:" + usage
		}
		if st.LatencyMS > 0 {
			row += fmt.Sprintf(" lat:%s", formatDurationMS(st.LatencyMS))
		}
		if st.TTFTMS > 0 {
			row += fmt.Sprintf(" ttft:%s", formatDurationMS(st.TTFTMS))
		}
		if st.TokenRate > 0 {
			row += fmt.Sprintf(" %.0ft/s", st.TokenRate)
		}
		if st.Queue > 0 {
			row += fmt.Sprintf(" q:%d", st.Queue)
		}
		if trace := shortTraceID(st.CurrentTrace, st.LastTrace); trace != "" {
			row += " tr:" + trace
		}
		if st.LastError != "" {
			row += " err:" + truncatePlain(st.LastError, 32)
		} else if st.LastThinking != "" {
			row += " think:" + truncatePlain(st.LastThinking, 32)
		}
		rows = append(rows, truncatePlain(row, width))
	}
	if len(rows) == 0 {
		return "observability: no active client sessions"
	}
	return strings.Join(rows, "\n")
}

func renderActiveClientBuffer(provider string, blocks []sessionDeckBlock, limit int) string {
	if provider == "" {
		return "(no active client)"
	}
	if len(blocks) == 0 {
		return fmt.Sprintf("[%s] no buffered output yet", provider)
	}
	if limit <= 0 || limit > len(blocks) {
		limit = len(blocks)
	}
	start := len(blocks) - limit
	var b strings.Builder
	fmt.Fprintf(&b, "[%s] buffered panel\n", provider)
	for _, block := range blocks[start:] {
		label := block.Kind
		if label == "" {
			label = "block"
		}
		fmt.Fprintf(&b, "\n%s %s\n%s\n", block.At.Format("15:04:05"), label, block.Text)
	}
	return strings.TrimRight(b.String(), "\n")
}

func fallbackDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "--"
	}
	return s
}

func truncatePlain(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	return s[:max-1] + "…"
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

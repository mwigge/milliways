package repl

import (
	"strings"
	"testing"
)

func TestMinimaxThinkFilter_NoThinkTags(t *testing.T) {
	t.Parallel()
	var f minimaxThinkFilter
	var got, thinking strings.Builder
	f.write("Hello world", func(s string) { got.WriteString(s) }, func(s string) { thinking.WriteString(s) })
	if got.String() != "Hello world" || thinking.Len() != 0 {
		t.Fatalf("got=%q thinking=%q", got.String(), thinking.String())
	}
}

func TestMinimaxThinkFilter_SingleChunk(t *testing.T) {
	t.Parallel()
	var f minimaxThinkFilter
	var got, thinking strings.Builder
	f.write("<think>reasoning here</think>answer", func(s string) { got.WriteString(s) }, func(s string) { thinking.WriteString(s) })
	if got.String() != "answer" {
		t.Fatalf("content = %q, want %q", got.String(), "answer")
	}
	if !strings.Contains(thinking.String(), "reasoning here") {
		t.Fatalf("thinking = %q, want 'reasoning here'", thinking.String())
	}
}

func TestMinimaxThinkFilter_SpansChunks(t *testing.T) {
	t.Parallel()
	var f minimaxThinkFilter
	var got, thinking strings.Builder
	write := func(s string) { got.WriteString(s) }
	think := func(s string) { thinking.WriteString(s) }

	f.write("<think>\nthought part one\n", write, think)
	f.write("thought part two\n</think>\n\nactual answer", write, think)

	if !strings.Contains(got.String(), "actual answer") {
		t.Fatalf("content missing answer: %q", got.String())
	}
	if strings.Contains(got.String(), "<think>") || strings.Contains(got.String(), "thought") {
		t.Fatalf("content leaked think block: %q", got.String())
	}
	if !strings.Contains(thinking.String(), "thought part one") {
		t.Fatalf("thinking missing content: %q", thinking.String())
	}
}

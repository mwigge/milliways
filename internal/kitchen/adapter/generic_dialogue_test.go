package adapter

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/mwigge/milliways/internal/kitchen"
)

func TestGenericAdapter_Exec_DetectsDialogueAndSendsAnswers(t *testing.T) {
	t.Parallel()

	script := writeGenericTestExecutable(t, t.TempDir(), "echo", `#!/bin/sh
printf 'before\n'
printf '?MW> Which runner?\n'
IFS= read -r question_answer
printf 'question answer: %s\n' "$question_answer"
printf '!MW> Continue?\n'
IFS= read -r confirm_answer
printf 'confirm answer: %s\n' "$confirm_answer"
`)

	adapter := NewGenericAdapter(newDialogueTestKitchen(script), AdapterOpts{})

	var prompts []string
	ch, err := adapter.Exec(context.Background(), kitchen.Task{
		Prompt: "",
		OnQuestion: func(question string) {
			prompts = append(prompts, "Q:"+question)
			if err := adapter.Send(context.Background(), "go test"); err != nil {
				t.Errorf("Send(question) failed: %v", err)
			}
		},
		OnConfirm: func(question string) {
			prompts = append(prompts, "C:"+question)
			if err := adapter.Send(context.Background(), "y"); err != nil {
				t.Errorf("Send(confirm) failed: %v", err)
			}
		},
	})
	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}

	var got []Event
	for evt := range ch {
		got = append(got, evt)
	}

	if !slices.Equal(prompts, []string{"Q:Which runner?", "C:Continue?"}) {
		t.Fatalf("prompts = %v, want question and confirm callbacks", prompts)
	}

	var eventTypes []EventType
	var eventTexts []string
	for _, evt := range got {
		eventTypes = append(eventTypes, evt.Type)
		eventTexts = append(eventTexts, evt.Text)
	}

	if !slices.Equal(eventTypes, []EventType{
		EventText,
		EventQuestion,
		EventText,
		EventConfirm,
		EventText,
		EventDone,
	}) {
		t.Fatalf("event types = %v", eventTypes)
	}

	if eventTexts[0] != "before" {
		t.Fatalf("first text = %q, want before", eventTexts[0])
	}
	if eventTexts[1] != "Which runner?" {
		t.Fatalf("question text = %q, want Which runner?", eventTexts[1])
	}
	if eventTexts[2] != "question answer: go test" {
		t.Fatalf("question answer text = %q", eventTexts[2])
	}
	if eventTexts[3] != "Continue?" {
		t.Fatalf("confirm text = %q, want Continue?", eventTexts[3])
	}
	if eventTexts[4] != "confirm answer: y" {
		t.Fatalf("confirm answer text = %q", eventTexts[4])
	}

	if got[len(got)-1].ExitCode != 0 {
		t.Fatalf("done exit code = %d, want 0", got[len(got)-1].ExitCode)
	}
}

func TestGenericAdapter_Exec_HeadlessAutoAnswer(t *testing.T) {
	t.Parallel()

	script := writeGenericTestExecutable(t, t.TempDir(), "echo", `#!/bin/sh
printf '?MW> Proceed?\n'
IFS= read -r answer
printf 'received:%s\n' "$answer"
`)

	adapter := NewGenericAdapter(newDialogueTestKitchen(script), AdapterOpts{})
	ch, err := adapter.Exec(context.Background(), kitchen.Task{Prompt: ""})
	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}

	var got []Event
	for evt := range ch {
		got = append(got, evt)
	}

	if len(got) != 3 {
		t.Fatalf("got %d events, want 3; events=%+v", len(got), got)
	}
	if got[0].Type != EventQuestion || got[0].Text != "Proceed?" {
		t.Fatalf("first event = %+v, want question Proceed?", got[0])
	}
	if got[1].Type != EventText || got[1].Text != "received:" {
		t.Fatalf("second event = %+v, want received empty answer", got[1])
	}
	if got[2].Type != EventDone || got[2].ExitCode != 0 {
		t.Fatalf("third event = %+v, want done exit 0", got[2])
	}
}

func newDialogueTestKitchen(cmd string) *kitchen.GenericKitchen {
	return kitchen.NewGeneric(kitchen.GenericConfig{
		Name:     "echo",
		Cmd:      cmd,
		Stations: []string{"test"},
		Tier:     kitchen.Free,
		Enabled:  true,
	})
}

func writeGenericTestExecutable(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatalf("WriteFile(%s): %v", name, err)
	}
	return path
}

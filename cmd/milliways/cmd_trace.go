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
	"encoding/json"
	"fmt"
	"time"

	"github.com/mwigge/milliways/internal/observability"
	"github.com/spf13/cobra"
)

type traceEventView struct {
	ID          string         `json:"id,omitempty"`
	Session     string         `json:"session,omitempty"`
	SessionID   string         `json:"session_id,omitempty"`
	Timestamp   string         `json:"timestamp,omitempty"`
	Type        string         `json:"type"`
	Description string         `json:"description,omitempty"`
	Actor       string         `json:"actor,omitempty"`
	Parent      string         `json:"parent,omitempty"`
	Data        map[string]any `json:"data,omitempty"`
}

func traceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trace",
		Short: "Inspect recorded trace sessions",
	}

	cmd.AddCommand(traceListCmd())
	cmd.AddCommand(traceShowCmd())
	cmd.AddCommand(traceDiagramCmd())

	return cmd
}

func traceListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available trace sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			sessions, err := observability.ListTraceSessions()
			if err != nil {
				return err
			}
			if len(sessions) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No trace sessions found.")
				return nil
			}
			for _, session := range sessions {
				fmt.Fprintln(cmd.OutOrStdout(), session)
			}
			return nil
		},
	}
}

func traceShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <session-id>",
		Short: "Show raw trace events as indented JSON",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			events, err := observability.ReadTraceEvents(args[0])
			if err != nil {
				return err
			}
			data, err := json.MarshalIndent(traceEventViews(events), "", "  ")
			if err != nil {
				return fmt.Errorf("marshal trace events: %w", err)
			}
			_, err = cmd.OutOrStdout().Write(append(data, '\n'))
			return err
		},
	}
}

func traceDiagramCmd() *cobra.Command {
	var graph bool
	var decision bool

	cmd := &cobra.Command{
		Use:   "diagram <session-id>",
		Short: "Render a Mermaid diagram for a trace session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			events, err := observability.ReadTraceEvents(args[0])
			if err != nil {
				return err
			}

			diagram := observability.GenerateTimeline(events)
			switch {
			case decision:
				diagram = observability.GenerateDecisionTree(events)
			case graph:
				diagram = observability.GenerateCallGraph(events)
			}

			_, err = fmt.Fprintln(cmd.OutOrStdout(), diagram)
			return err
		},
	}

	cmd.Flags().BoolVar(&graph, "graph", false, "Output a Mermaid call graph")
	cmd.Flags().BoolVar(&decision, "decision", false, "Output a Mermaid decision tree")
	cmd.MarkFlagsMutuallyExclusive("graph", "decision")
	return cmd
}

func traceEventViews(events []observability.AgentTraceEvent) []traceEventView {
	views := make([]traceEventView, 0, len(events))
	for _, event := range events {
		view := traceEventView{
			ID:          event.ID,
			Session:     event.SessionID,
			SessionID:   event.SessionID,
			Type:        event.Type,
			Description: event.Description,
			Actor:       event.Actor,
			Parent:      event.Parent,
			Data:        event.Data,
		}
		if !event.Timestamp.IsZero() {
			view.Timestamp = event.Timestamp.UTC().Format(time.RFC3339Nano)
		}
		views = append(views, view)
	}
	return views
}

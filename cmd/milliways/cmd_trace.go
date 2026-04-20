package main

import (
	"encoding/json"
	"fmt"

	"github.com/mwigge/milliways/internal/observability"
	"github.com/spf13/cobra"
)

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
			data, err := json.MarshalIndent(events, "", "  ")
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

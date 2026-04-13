package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mwigge/milliways/internal/kitchen"
	"github.com/mwigge/milliways/internal/ledger"
	"github.com/mwigge/milliways/internal/maitre"
	"github.com/mwigge/milliways/internal/pantry"
	"github.com/mwigge/milliways/internal/sommelier"
	"github.com/spf13/cobra"
)

var version = "0.1.0"

// exitError wraps an error with a specific exit code.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }

func main() {
	if err := rootCmd().Execute(); err != nil {
		if ee, ok := err.(*exitError); ok {
			os.Exit(ee.code)
		}
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	var (
		kitchenFlag string
		jsonFlag    bool
		explainFlag bool
		configPath  string
		verbose     bool
	)

	cmd := &cobra.Command{
		Use:   "milliways [prompt]",
		Short: "The Restaurant at the End of the Universe — one CLI to route them all",
		Long: `Milliways seats you at the right table. It doesn't cook — it routes
your task to the best kitchen (claude, opencode, gemini, aider, goose)
based on what each tool does best.

  milliways "explain the auth flow"        → routes to claude
  milliways "code a rate limiter"          → routes to opencode
  milliways "search for DORA-EU Article 25" → routes to gemini
  milliways --kitchen aider "refactor auth" → forces aider`,
		Version: version,
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			prompt := strings.Join(args, " ")
			return dispatch(prompt, kitchenFlag, jsonFlag, explainFlag, verbose, configPath)
		},
		SilenceUsage: true,
	}

	cmd.Flags().StringVarP(&kitchenFlag, "kitchen", "k", "", "Force a specific kitchen (e.g., claude, opencode, gemini)")
	cmd.Flags().BoolVarP(&jsonFlag, "json", "j", false, "Output structured JSON result")
	cmd.Flags().BoolVarP(&explainFlag, "explain", "e", false, "Show routing decision without executing")
	cmd.Flags().StringVarP(&configPath, "config", "c", maitre.DefaultConfigPath(), "Path to carte.yaml")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Print sommelier reasoning to stderr")

	cmd.AddCommand(statusCmd(&configPath))
	cmd.AddCommand(reportCmd(&configPath))
	cmd.AddCommand(setupCmd(&configPath))

	return cmd
}

func dispatch(prompt, kitchenForce string, jsonOutput, explain, verbose bool, configPath string) error {
	cfg, err := maitre.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	reg := buildRegistry(cfg)
	som := sommelier.New(cfg.Routing.Keywords, cfg.Routing.Default, cfg.Routing.BudgetFallback, reg)

	var decision sommelier.Decision
	if kitchenForce != "" {
		decision = som.ForceRoute(kitchenForce)
	} else {
		decision = som.Route(prompt)
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "[sommelier] %s (tier: %s)\n", decision.Reason, decision.Tier)
	}

	if explain {
		return printJSON(decision, jsonOutput)
	}

	if decision.Kitchen == "" {
		return fmt.Errorf("no kitchens available — run 'milliways status' to check")
	}

	k, ok := reg.Get(decision.Kitchen)
	if !ok {
		return fmt.Errorf("kitchen %q not found in registry", decision.Kitchen)
	}

	if k.Status() != kitchen.Ready {
		return fmt.Errorf("kitchen %q is %s — run 'milliways --setup %s' to fix", decision.Kitchen, k.Status(), decision.Kitchen)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	task := kitchen.Task{
		Prompt: prompt,
		OnLine: func(line string) {
			if !jsonOutput {
				fmt.Println(line)
			}
		},
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "[dispatch] %s streaming...\n", decision.Kitchen)
	}

	start := time.Now()
	result, execErr := k.Exec(ctx, task)
	duration := time.Since(start).Seconds()

	if verbose {
		fmt.Fprintf(os.Stderr, "[dispatch] %s done (%.1fs, exit=%d)\n", decision.Kitchen, duration, result.ExitCode)
	}

	// Write to unified PantryDB + ndjson audit trail (best-effort)
	dbPath := filepath.Join(maitre.DefaultConfigDir(), "milliways.db")
	pdb, pdbErr := pantry.Open(dbPath)
	if pdbErr != nil {
		fmt.Fprintf(os.Stderr, "[pantry] warning: %v\n", pdbErr)
	} else {
		defer func() { _ = pdb.Close() }()
		entry := pantry.LedgerEntry{
			Timestamp:    time.Now().UTC().Format(time.RFC3339),
			TaskHash:     ledger.HashPrompt(prompt),
			Kitchen:      decision.Kitchen,
			DurationSec:  duration,
			ExitCode:     result.ExitCode,
			Outcome:      outcomeFromExit(result.ExitCode),
			DispatchMode: "sync",
		}
		if _, writeErr := pdb.Ledger().Insert(entry); writeErr != nil {
			fmt.Fprintf(os.Stderr, "[pantry] ledger warning: %v\n", writeErr)
		}
		if quotaErr := pdb.Quotas().Increment(decision.Kitchen, duration, result.ExitCode != 0); quotaErr != nil {
			fmt.Fprintf(os.Stderr, "[pantry] quota warning: %v\n", quotaErr)
		}
	}
	// ndjson audit trail (human-readable, never read by Milliways)
	ndjsonEntry := ledger.NewEntry(prompt, decision.Kitchen, "", duration, result.ExitCode)
	nw := ledger.NewWriter(cfg.Ledger.NDJSON)
	if writeErr := nw.Write(ndjsonEntry); writeErr != nil {
		fmt.Fprintf(os.Stderr, "[ledger] ndjson warning: %v\n", writeErr)
	}

	if execErr != nil {
		return fmt.Errorf("kitchen %s: %w", decision.Kitchen, execErr)
	}

	if jsonOutput {
		out := map[string]any{
			"kitchen":    decision.Kitchen,
			"reason":     decision.Reason,
			"tier":       decision.Tier,
			"exit_code":  result.ExitCode,
			"duration_s": duration,
			"output":     result.Output,
		}
		if err := printJSON(out, true); err != nil {
			return err
		}
	}

	if result.ExitCode != 0 {
		return &exitError{code: result.ExitCode, err: fmt.Errorf("kitchen %s exited with code %d", decision.Kitchen, result.ExitCode)}
	}

	return nil
}

func printJSON(v any, asJSON bool) error {
	if !asJSON {
		switch d := v.(type) {
		case sommelier.Decision:
			fmt.Printf("Kitchen: %s\nReason:  %s\nTier:    %s\n", d.Kitchen, d.Reason, d.Tier)
			return nil
		default:
			fmt.Printf("%v\n", v)
			return nil
		}
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling output: %w", err)
	}
	fmt.Println(string(data))
	return nil
}

func outcomeFromExit(exitCode int) string {
	if exitCode == 0 {
		return "success"
	}
	return "failure"
}

func openPantryDB() (*pantry.DB, error) {
	dbPath := filepath.Join(maitre.DefaultConfigDir(), "milliways.db")
	return pantry.Open(dbPath)
}

func buildRegistry(cfg *maitre.Config) *kitchen.Registry {
	reg := kitchen.NewRegistry()

	installCmds := map[string]string{
		"claude":   "brew install claude",
		"opencode": "brew install opencode",
		"gemini":   "npm install -g @google/gemini-cli",
		"aider":    "pip install aider-chat",
		"goose":    "brew install goose",
		"cline":    "npm install -g cline",
	}

	authCmds := map[string]string{
		"claude":   "claude (interactive login)",
		"opencode": "none (uses Ollama)",
		"gemini":   "gcloud auth login",
		"aider":    "set ANTHROPIC_API_KEY or OPENAI_API_KEY",
		"goose":    "goose configure",
		"cline":    "cline --login",
	}

	for name, kc := range cfg.Kitchens {
		reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{
			Name:       name,
			Cmd:        kc.Cmd,
			Args:       kc.Args,
			Stations:   kc.Stations,
			Tier:       kitchen.ParseCostTier(kc.CostTier),
			Enabled:    kc.IsEnabled(),
			InstallCmd: installCmds[name],
			AuthCmd:    authCmds[name],
		}))
	}

	return reg
}

func statusCmd(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show kitchen availability, pantry health, and ledger stats",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := maitre.LoadConfig(*configPath)
			if err != nil {
				return err
			}
			reg := buildRegistry(cfg)
			health := maitre.Diagnose(reg)
			maitre.PrintStatus(health)

			// Show ledger stats from PantryDB
			pdb, pdbErr := openPantryDB()
			if pdbErr == nil {
				defer func() { _ = pdb.Close() }()
				total, _ := pdb.Ledger().Total()
				if total > 0 {
					fmt.Printf("\nLedger: %d entries\n", total)
				}
			}

			return nil
		},
	}
}

func setupCmd(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "setup <kitchen>",
		Short: "Install and authenticate a kitchen",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := maitre.LoadConfig(*configPath)
			if err != nil {
				return err
			}
			reg := buildRegistry(cfg)

			k, ok := reg.Get(args[0])
			if !ok {
				return fmt.Errorf("unknown kitchen %q — run 'milliways status' to see available kitchens", args[0])
			}

			return maitre.SetupKitchen(k)
		},
	}
}

func reportCmd(_ *string) *cobra.Command {
	return &cobra.Command{
		Use:   "report",
		Short: "Show routing stats from the ledger",
		RunE: func(cmd *cobra.Command, args []string) error {
			pdb, err := openPantryDB()
			if err != nil {
				return fmt.Errorf("opening pantry: %w", err)
			}
			defer func() { _ = pdb.Close() }()

			total, err := pdb.Ledger().Total()
			if err != nil {
				return err
			}
			if total == 0 {
				fmt.Println("No ledger entries yet. Start dispatching tasks!")
				return nil
			}

			stats, err := pdb.Ledger().Stats()
			if err != nil {
				return err
			}

			fmt.Printf("Ledger: %d entries\n\n", total)
			fmt.Println("Kitchen      Dispatches  Success Rate")
			fmt.Println("───────      ──────────  ────────────")

			for _, ks := range stats {
				fmt.Printf("%-12s %10d  %11.0f%%\n", ks.Kitchen, ks.Dispatches, ks.SuccessRate)
			}

			return nil
		},
	}
}

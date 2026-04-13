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

// dispatchOpts groups the parameters for the dispatch function.
type dispatchOpts struct {
	prompt, kitchenForce, configPath string
	jsonOutput, explain, verbose     bool
}

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
		recipeFlag  string
		asyncFlag   bool
		detachFlag  bool
		keepContext bool
		tuiFlag     bool
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
		Args:    cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if tuiFlag {
				return runTUI(configPath)
			}
			if len(args) == 0 {
				return cmd.Help()
			}
			prompt := strings.Join(args, " ")
			if recipeFlag != "" {
				return dispatchRecipe(recipeFlag, prompt, verbose, configPath, keepContext)
			}
			if asyncFlag {
				return dispatchAsync(prompt, kitchenFlag, verbose, configPath)
			}
			if detachFlag {
				return dispatchDetach(prompt, kitchenFlag, verbose, configPath)
			}
			return dispatch(dispatchOpts{
				prompt:       prompt,
				kitchenForce: kitchenFlag,
				configPath:   configPath,
				jsonOutput:   jsonFlag,
				explain:      explainFlag,
				verbose:      verbose,
			})
		},
		SilenceUsage: true,
	}

	cmd.Flags().StringVarP(&kitchenFlag, "kitchen", "k", "", "Force a specific kitchen (e.g., claude, opencode, gemini)")
	cmd.Flags().BoolVarP(&jsonFlag, "json", "j", false, "Output structured JSON result")
	cmd.Flags().BoolVarP(&explainFlag, "explain", "e", false, "Show routing decision without executing")
	cmd.Flags().StringVarP(&configPath, "config", "c", maitre.DefaultConfigPath(), "Path to carte.yaml")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Print sommelier reasoning to stderr")
	cmd.Flags().StringVarP(&recipeFlag, "recipe", "r", "", "Execute a multi-course recipe")
	cmd.Flags().BoolVar(&asyncFlag, "async", false, "Dispatch asynchronously, return ticket ID")
	cmd.Flags().BoolVar(&detachFlag, "detach", false, "Dispatch detached (survives exit)")
	cmd.Flags().BoolVar(&keepContext, "keep-context", false, "Keep recipe context files")
	cmd.Flags().BoolVarP(&tuiFlag, "tui", "t", false, "Interactive TUI mode")

	cmd.AddCommand(statusCmd(&configPath))
	cmd.AddCommand(reportCmd(&configPath))
	cmd.AddCommand(setupCmd(&configPath))
	cmd.AddCommand(pantryCmd())
	cmd.AddCommand(ticketCmd())
	cmd.AddCommand(ticketsCmd())

	return cmd
}

func dispatch(opts dispatchOpts) error {
	cfg, err := maitre.LoadConfig(opts.configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Open PantryDB once — used for signals assembly and post-dispatch recording.
	pdb, pdbErr := openPantryDB()
	if pdbErr != nil && opts.verbose {
		fmt.Fprintf(os.Stderr, "[pantry] warning: %v\n", pdbErr)
	}
	if pdb != nil {
		defer func() { _ = pdb.Close() }()
	}

	reg := buildRegistry(cfg)
	som := sommelier.New(cfg.Routing.Keywords, cfg.Routing.Default, cfg.Routing.BudgetFallback, reg)

	// Circuit breaker check
	mode := maitre.ReadMode()
	if opts.verbose {
		fmt.Fprintf(os.Stderr, "[mode] %s\n", mode)
	}

	if pathErr := checkPromptPaths(opts.prompt, mode); pathErr != nil {
		return pathErr
	}

	var decision sommelier.Decision
	if opts.kitchenForce != "" {
		decision = som.ForceRoute(opts.kitchenForce)
	} else {
		signals := assembleSignals(cfg, pdb, opts.prompt, opts.verbose)

		var skillHint *sommelier.SkillHint
		catalog := maitre.ScanSkills()
		if catalog.Total() > 0 {
			if kitchenName, skill := catalog.HasSkill(opts.prompt); skill != nil {
				skillHint = &sommelier.SkillHint{Kitchen: kitchenName, SkillName: skill.Name}
				if opts.verbose {
					fmt.Fprintf(os.Stderr, "[skills] %q matches skill %q in %s\n", opts.prompt, skill.Name, kitchenName)
				}
			}
		}

		decision = som.RouteEnriched(opts.prompt, signals, skillHint)
	}

	if opts.verbose {
		fmt.Fprintf(os.Stderr, "[sommelier] %s (tier: %s, risk: %s)\n", decision.Reason, decision.Tier, decision.Risk)
	}

	if opts.explain {
		return printJSON(decision, opts.jsonOutput)
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
		Prompt: opts.prompt,
		Env:    map[string]string{"MILLIWAYS_MODE": string(mode)},
		OnLine: func(line string) {
			if !opts.jsonOutput {
				fmt.Println(line)
			}
		},
	}

	if opts.verbose {
		fmt.Fprintf(os.Stderr, "[dispatch] %s streaming...\n", decision.Kitchen)
	}

	start := time.Now()
	result, execErr := k.Exec(ctx, task)
	duration := time.Since(start).Seconds()

	if opts.verbose {
		fmt.Fprintf(os.Stderr, "[dispatch] %s done (%.1fs, exit=%d)\n", decision.Kitchen, duration, result.ExitCode)
	}

	recordDispatch(cfg, pdb, opts.prompt, decision.Kitchen, duration, result.ExitCode)

	if execErr != nil {
		return fmt.Errorf("kitchen %s: %w", decision.Kitchen, execErr)
	}

	if opts.jsonOutput {
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

// recordDispatch writes to PantryDB + ndjson audit trail + routing feedback.
func recordDispatch(cfg *maitre.Config, pdb *pantry.DB, prompt, kitchenName string, duration float64, exitCode int) {
	taskType := sommelier.ClassifyTaskType(prompt)
	outcome := ledger.OutcomeFromExitCode(exitCode)

	if pdb != nil {
		entry := pantry.LedgerEntry{
			Timestamp:    time.Now().UTC().Format(time.RFC3339),
			TaskHash:     ledger.HashPrompt(prompt),
			TaskType:     taskType,
			Kitchen:      kitchenName,
			DurationSec:  duration,
			ExitCode:     exitCode,
			Outcome:      outcome,
			DispatchMode: "sync",
		}
		if _, writeErr := pdb.Ledger().Insert(entry); writeErr != nil {
			fmt.Fprintf(os.Stderr, "[pantry] ledger warning: %v\n", writeErr)
		}
		if quotaErr := pdb.Quotas().Increment(kitchenName, duration, exitCode != 0); quotaErr != nil {
			fmt.Fprintf(os.Stderr, "[pantry] quota warning: %v\n", quotaErr)
		}
		if routeErr := pdb.Routing().RecordOutcome(taskType, "", kitchenName, exitCode == 0, duration); routeErr != nil {
			fmt.Fprintf(os.Stderr, "[pantry] routing warning: %v\n", routeErr)
		}
	}

	ndjsonEntry := ledger.NewEntry(prompt, kitchenName, "", duration, exitCode)
	nw := ledger.NewWriter(cfg.Ledger.NDJSON)
	if writeErr := nw.Write(ndjsonEntry); writeErr != nil {
		fmt.Fprintf(os.Stderr, "[ledger] ndjson warning: %v\n", writeErr)
	}
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

func assembleSignals(_ *maitre.Config, pdb *pantry.DB, prompt string, verbose bool) *sommelier.Signals {
	if pdb == nil {
		if verbose {
			fmt.Fprintf(os.Stderr, "[pantry] signals unavailable: no database\n")
		}
		return nil
	}

	signals := sommelier.NewSignals()

	taskType := sommelier.ClassifyTaskType(prompt)
	best, rate, err := pdb.Routing().BestKitchen(taskType, "", 5)
	if err == nil && best != "" {
		signals.LearnedKitchen = best
		signals.LearnedRate = rate
	}

	if verbose && signals.LearnedKitchen != "" {
		fmt.Fprintf(os.Stderr, "[pantry] learned: %s@%.0f%% for task_type=%s\n", signals.LearnedKitchen, signals.LearnedRate, taskType)
	}

	return signals
}

func openPantryDB() (*pantry.DB, error) {
	dbPath := filepath.Join(maitre.DefaultConfigDir(), "milliways.db")
	return pantry.Open(dbPath)
}

// checkPromptPaths scans for absolute paths in the prompt and enforces
// the circuit breaker on each one. Returns the first violation found.
func checkPromptPaths(prompt string, mode maitre.Mode) error {
	for _, word := range strings.Fields(prompt) {
		isAbsolute := strings.HasPrefix(word, "/")
		isHome := strings.HasPrefix(word, "~/")
		if !isAbsolute && !isHome {
			continue
		}
		path := word
		if isHome {
			home, err := os.UserHomeDir()
			if err != nil {
				continue
			}
			path = filepath.Join(home, word[2:])
		}
		if err := maitre.PathAllowed(path, mode); err != nil {
			return fmt.Errorf("circuit breaker: %w", err)
		}
	}
	return nil
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
	var tiered bool

	cmd := &cobra.Command{
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

			if tiered {
				fmt.Println()
				printTieredReport(pdb)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&tiered, "tiered", false, "Show tiered-CLI performance analysis")
	return cmd
}

func printTieredReport(pdb *pantry.DB) {
	tieredStats, err := pdb.Ledger().TieredStats()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[report] tiered query failed: %v\n", err)
		return
	}

	bestPerType := make(map[string]*pantry.TaskKitchenStat)
	kitchenTotals := make(map[string]struct{ dispatches, successes int })

	for i := range tieredStats {
		s := &tieredStats[i]
		// Track best kitchen per task type
		if best, ok := bestPerType[s.TaskType]; !ok || s.Rate > best.Rate {
			bestPerType[s.TaskType] = s
		}

		// Track overall per kitchen
		totals := kitchenTotals[s.Kitchen]
		totals.dispatches += s.Dispatches
		totals.successes += s.Successes
		kitchenTotals[s.Kitchen] = totals
	}

	if len(bestPerType) == 0 {
		fmt.Println("Tiered-CLI: insufficient data (dispatch more tasks with varied types)")
		return
	}

	fmt.Println("Tiered-CLI Performance")
	fmt.Println("══════════════════════")
	fmt.Println()
	fmt.Println("Task Type    Best Kitchen   Success  Dispatches")
	fmt.Println("─────────    ────────────   ───────  ──────────")

	multiCLISuccess := 0
	multiCLITotal := 0
	for _, best := range bestPerType {
		fmt.Printf("%-12s %-14s %5.0f%%   %d\n", best.TaskType, best.Kitchen, best.Rate, best.Dispatches)
		multiCLISuccess += best.Successes
		multiCLITotal += best.Dispatches
	}

	// Compute best single-CLI score
	bestSingleRate := 0.0
	bestSingleName := ""
	for name, totals := range kitchenTotals {
		if totals.dispatches > 0 {
			rate := float64(totals.successes) / float64(totals.dispatches) * 100
			if rate > bestSingleRate {
				bestSingleRate = rate
				bestSingleName = name
			}
		}
	}

	multiCLIRate := 0.0
	if multiCLITotal > 0 {
		multiCLIRate = float64(multiCLISuccess) / float64(multiCLITotal) * 100
	}

	fmt.Println()
	fmt.Printf("Multi-CLI composite:  %.1f%%\n", multiCLIRate)
	fmt.Printf("Best single-CLI:     %.1f%% (%s)\n", bestSingleRate, bestSingleName)
	lift := multiCLIRate - bestSingleRate
	if lift > 0 {
		fmt.Printf("Tiered-CLI lift:     +%.1f%%\n", lift)
	} else {
		fmt.Printf("Tiered-CLI lift:     %.1f%% (need more data or varied tasks)\n", lift)
	}
}

func pantryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pantry",
		Short: "Manage pantry knowledge stores",
	}

	syncCmd := &cobra.Command{
		Use:   "sync [repo-path]",
		Short: "Sync GitGraph from git history for a repository",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoPath := "."
			if len(args) > 0 {
				repoPath = args[0]
			}

			// Validate that the path is a git repository
			gitDir := filepath.Join(repoPath, ".git")
			if _, err := os.Stat(gitDir); os.IsNotExist(err) {
				return fmt.Errorf("%s is not a git repository (no .git directory)", repoPath)
			}

			pdb, err := openPantryDB()
			if err != nil {
				return fmt.Errorf("opening pantry: %w", err)
			}
			defer func() { _ = pdb.Close() }()

			count, err := pdb.GitGraph().Sync(repoPath)
			if err != nil {
				return fmt.Errorf("syncing gitgraph: %w", err)
			}

			fmt.Printf("GitGraph: synced %d files from %s\n", count, repoPath)
			return nil
		},
	}

	depSyncCmd := &cobra.Command{
		Use:   "deps [repo-path]",
		Short: "Sync DepGraph from lock files (go.mod, package.json)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoPath := "."
			if len(args) > 0 {
				repoPath = args[0]
			}

			pdb, err := openPantryDB()
			if err != nil {
				return fmt.Errorf("opening pantry: %w", err)
			}
			defer func() { _ = pdb.Close() }()

			count, err := pdb.Deps().SyncAuto(repoPath)
			if err != nil {
				return fmt.Errorf("syncing deps: %w", err)
			}

			fmt.Printf("DepGraph: synced %d dependencies from %s\n", count, repoPath)
			return nil
		},
	}

	cmd.AddCommand(syncCmd)
	cmd.AddCommand(depSyncCmd)
	return cmd
}

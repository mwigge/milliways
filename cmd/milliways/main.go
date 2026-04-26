package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/mwigge/milliways/internal/bridge"
	"github.com/mwigge/milliways/internal/conversation"
	"github.com/mwigge/milliways/internal/editorcontext"
	"github.com/mwigge/milliways/internal/kitchen"
	"github.com/mwigge/milliways/internal/kitchen/adapter"
	"github.com/mwigge/milliways/internal/ledger"
	"github.com/mwigge/milliways/internal/maitre"
	"github.com/mwigge/milliways/internal/migration"
	"github.com/mwigge/milliways/internal/orchestrator"
	"github.com/mwigge/milliways/internal/pantry"
	"github.com/mwigge/milliways/internal/project"
	"github.com/mwigge/milliways/internal/sommelier"
	"github.com/mwigge/milliways/internal/repl"
	"github.com/mwigge/milliways/internal/substrate"
	"github.com/spf13/cobra"
)

var version = "0.4.7"

// dispatchOpts groups the parameters for the dispatch function.
type dispatchOpts struct {
	prompt, kitchenForce, configPath, projectRoot string
	contextJSON, contextFile                      string
	jsonOutput, explain, verbose, contextStdin    bool
	useLegacyConversation                         bool
	bundle                                        *editorcontext.Bundle
	timeout                                       time.Duration
}

// exitError wraps an error with a specific exit code.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))
	if err := rootCmd().Execute(); err != nil {
		var ee *exitError
		if errors.As(err, &ee) {
			os.Exit(ee.code)
		}
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	var (
		kitchenFlag           string
		jsonFlag              bool
		explainFlag           bool
		configPath            string
		verbose               bool
		recipeFlag            string
		asyncFlag             bool
		detachFlag            bool
		keepContext           bool
		replFlag              bool
		useLegacyConversation bool
		projectRoot           string
		contextJSON           string
		contextFile           string
		contextStdin          bool
		timeoutDur            time.Duration
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
	  milliways login <kitchen>              → authenticate to a specific kitchen
  milliways --kitchen aider "refactor auth" → forces aider`,
		Version: version,
		Args:    cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// REPL is the default when no prompt args are given.
			if len(args) == 0 || replFlag {
				noRestore, _ := cmd.Flags().GetBool("no-restore")
				return runREPL(configPath, noRestore)
			}
			projectContext, err := project.ResolveProject(projectRoot)
			if err != nil {
				return err
			}
			if verbose {
				fmt.Fprintf(os.Stderr, "[project] root=%s repo=%s read=%s write=%s\n", projectContext.RepoRoot, projectContext.RepoName, projectContext.AccessRules.Read, projectContext.AccessRules.Write)
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
			bundle, err := loadDispatchContextBundle(os.Stdin, contextStdin, contextJSON, contextFile)
			if err != nil {
				return err
			}
			return dispatch(dispatchOpts{
				prompt:                prompt,
				kitchenForce:          kitchenFlag,
				configPath:            configPath,
				projectRoot:           projectRoot,
				contextJSON:           contextJSON,
				contextFile:           contextFile,
				jsonOutput:            jsonFlag,
				explain:               explainFlag,
				verbose:               verbose,
				contextStdin:          contextStdin,
				useLegacyConversation: useLegacyConversation,
				bundle:                bundle,
				timeout:               timeoutDur,
			})
		},
		SilenceUsage: true,
	}

	cmd.Flags().StringVarP(&kitchenFlag, "kitchen", "k", "", "Force a specific kitchen (e.g., claude, opencode, gemini)")
	cmd.Flags().BoolVarP(&jsonFlag, "json", "j", false, "Output structured JSON result")
	cmd.Flags().BoolVarP(&explainFlag, "explain", "e", false, "Show routing decision without executing")
	cmd.PersistentFlags().StringVarP(&configPath, "config", "c", maitre.DefaultConfigPath(), "Path to carte.yaml")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Print sommelier reasoning to stderr")
	cmd.Flags().StringVarP(&recipeFlag, "recipe", "r", "", "Execute a multi-course recipe")
	cmd.Flags().BoolVar(&asyncFlag, "async", false, "Dispatch asynchronously, return ticket ID")
	cmd.Flags().BoolVar(&detachFlag, "detach", false, "Dispatch detached (survives exit)")
	cmd.Flags().BoolVar(&keepContext, "keep-context", false, "Keep recipe context files")
	cmd.Flags().BoolVar(&replFlag, "repl", false, "Interactive REPL mode (default when no args)")
	cmd.Flags().StringVar(&projectRoot, "project-root", "", "Override project repository root")
	cmd.Flags().StringVar(&contextJSON, "context-json", "", "Pass editor context bundle JSON directly on the CLI")
	cmd.Flags().StringVar(&contextFile, "context-file", "", "Read editor context bundle JSON from a file")
	cmd.Flags().BoolVar(&contextStdin, "context-stdin", false, "Read editor context bundle JSON from stdin")
	cmd.Flags().BoolVar(&useLegacyConversation, "use-legacy-conversation", false, "Use pantry conversation storage instead of substrate")
	cmd.Flags().DurationVar(&timeoutDur, "timeout", 5*time.Minute, "Dispatch timeout for headless mode")
	cmd.Flags().Bool("no-restore", false, "Do not auto-restore the last session on startup")

	cmd.AddCommand(statusCmd(&configPath))
	cmd.AddCommand(reportCmd(&configPath))
	cmd.AddCommand(setupCmd(&configPath))
	cmd.AddCommand(pantryCmd())
	cmd.AddCommand(ticketCmd())
	cmd.AddCommand(ticketsCmd())
	cmd.AddCommand(rateCmd())
	cmd.AddCommand(loginCmd())
	cmd.AddCommand(initCmd())
	cmd.AddCommand(modeCmd())
	cmd.AddCommand(traceCmd())

	return cmd
}

func loginCmd() *cobra.Command {
	var listFlag bool

	cmd := &cobra.Command{
		Use:   "login <kitchen>",
		Short: "Authenticate to a kitchen",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if listFlag {
				configPath, err := cmd.Root().PersistentFlags().GetString("config")
				if err != nil || strings.TrimSpace(configPath) == "" {
					return listLoginStatus()
				}
				return listLoginStatusWithConfig(configPath)
			}
			if len(args) == 0 {
				fmt.Println("usage: milliways login <kitchen>")
				fmt.Println("  milliways login --list  show all kitchens and auth status")
				return fmt.Errorf("kitchen name required")
			}

			kitchenName := args[0]
			if err := maitre.LoginKitchen(kitchenName); err != nil {
				fmt.Fprintf(os.Stderr, "Login failed: %v\n", err)
				return err
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&listFlag, "list", false, "List all kitchens with auth status")
	return cmd
}

func listLoginStatus() error {
	return listLoginStatusWithConfig(maitre.DefaultConfigPath())
}

func listLoginStatusWithConfig(configPath string) error {
	cfg, err := maitre.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	health := maitre.Diagnose(buildRegistry(cfg))
	sort.Slice(health, func(i, j int) bool {
		return health[i].Name < health[j].Name
	})

	fmt.Println("Kitchen      Status              Auth Method           Action")
	fmt.Println("───────      ──────              ───────────           ──────")
	for _, h := range health {
		fmt.Printf("%-12s %s %-18s %-21s %s\n",
			h.Name,
			h.Status.Symbol(),
			h.Status,
			authMethodForKitchen(h.Name),
			loginActionForKitchen(h),
		)
	}

	return nil
}

func authMethodForKitchen(name string) string {
	switch name {
	case "claude", "gemini":
		return "Browser OAuth"
	case "opencode":
		return "Interactive TUI"
	case "minimax":
		return "API key (carte.yaml)"
	case "groq":
		return "Env var (GROQ_API_KEY)"
	case "ollama":
		return "None"
	case "aider", "cline":
		return "Env var (ANTHROPIC_API_KEY)"
	case "goose":
		return "Env var (GOOSE_API_KEY)"
	default:
		return "Unknown"
	}
}

func loginActionForKitchen(h maitre.KitchenHealth) string {
	switch h.Status {
	case kitchen.Ready:
		return "ready"
	case kitchen.Disabled:
		return "(disabled in carte.yaml)"
	case kitchen.NotInstalled:
		if h.InstallCmd != "" {
			return h.InstallCmd
		}
		return fmt.Sprintf("milliways setup %s", h.Name)
	case kitchen.NeedsAuth:
		return fmt.Sprintf("milliways login %s", h.Name)
	default:
		return "check configuration"
	}
}

func dispatch(opts dispatchOpts) error {
	cfg, err := maitre.LoadConfig(opts.configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	reg := buildRegistry(cfg)

	// Open PantryDB once — used for signals assembly and post-dispatch recording.
	pdb, pdbErr := openPantryDB()
	if pdbErr != nil && opts.verbose {
		fmt.Fprintf(os.Stderr, "[pantry] warning: %v\n", pdbErr)
	}
	if pdb != nil {
		pantryDB := pdb
		defer func() { _ = pantryDB.Close() }()
	}

	var recordLegacyConversation = true
	var hydrator orchestrator.ContextHydrator = makeConversationHydrator(pdb, opts.prompt)
	var sink = makeRuntimeSink(pdb)
	var substrateReader substrate.Reader
	projectContext, err := project.ResolveProject(opts.projectRoot)
	if err != nil {
		return err
	}
	projectBridge, err := bridge.New(projectContext, cfg.ProjectContextLimit)
	if err != nil && projectContext.PalacePath != nil {
		return err
	}
	if projectBridge != nil {
		defer func() { _ = projectBridge.Close() }()
	}

	if !opts.useLegacyConversation {
		substrateClient, err := openSubstrateClient()
		if err != nil {
			return err
		}
		defer func() { _ = substrateClient.Close() }()

		if pdb != nil {
			legacyDB, err := openLegacyConversationDB(pdb.Path())
			if err != nil {
				return err
			}
			defer func() { _ = legacyDB.Close() }()

			if err := migration.MigrateOnce(context.Background(), legacyDB, substrateClient); err != nil {
				return err
			}
			rowsMigrated, err := countLegacyConversationRows(legacyDB)
			if err != nil {
				return err
			}
			if _, err := legacyDB.Exec(`PRAGMA query_only = ON`); err != nil {
				return fmt.Errorf("setting legacy conversation db read-only: %w", err)
			}
			slog.Info("legacy-conversation-migration-complete", "rows_migrated", rowsMigrated)
		}

		recordLegacyConversation = false
		pdb = nil
		hydrator = nil
		sink = nil
		substrateReader = substrate.NewCachedReader(substrateClient)
	}

	som := sommelier.New(cfg.Routing.Keywords, cfg.Routing.Default, cfg.Routing.BudgetFallback, cfg.Routing.WeightOn, reg)

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
		signals := assembleSignals(cfg, pdb, opts.prompt, opts.verbose, opts.bundle)

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

	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(sigCtx, opts.timeout)
	defer cancel()

	if opts.verbose {
		fmt.Fprintf(os.Stderr, "[routed] %s\n", decision.Kitchen)
		fmt.Fprintf(os.Stderr, "[dispatch] %s streaming...\n", decision.Kitchen)
	}

	providerFactory := makeProviderFactory(cfg, reg, som, pdb, opts.verbose)
	orch := orchestrator.Orchestrator{
		Factory:        providerFactory,
		Hydrate:        hydrator,
		Sink:           sink,
		Reader:         substrateReader,
		Bridge:         projectBridge,
		ProjectContext: projectContext,
		MaxKitchens:   len(reg.Kitchens()),
	}

	start := time.Now()
	var output strings.Builder
	var costInfo *adapter.CostInfo
	exitCode := 0
	lastKitchen := decision.Kitchen

	conv, runErr := orch.Run(ctx, orchestrator.RunRequest{
		ConversationID: fmt.Sprintf("sync-%d", time.Now().UnixNano()),
		BlockID:        "sync",
		Prompt:         opts.prompt,
		KitchenForce:   opts.kitchenForce,
	}, func(res orchestrator.RouteResult) {
		lastKitchen = res.Decision.Kitchen
		if opts.verbose {
			fmt.Fprintf(os.Stderr, "[routed] %s\n", res.Decision.Kitchen)
		}
	}, func(evt adapter.Event) {
		switch evt.Type {
		case adapter.EventText:
			if !opts.jsonOutput {
				fmt.Println(evt.Text)
			}
			output.WriteString(evt.Text)
			output.WriteString("\n")
		case adapter.EventCodeBlock:
			if !opts.jsonOutput {
				fmt.Printf("```%s\n%s\n```\n", evt.Language, evt.Code)
			}
			output.WriteString(evt.Code)
			output.WriteString("\n")
		case adapter.EventCost:
			costInfo = evt.Cost
			if opts.verbose {
				fmt.Fprintf(os.Stderr, "[cost] $%.4f\n", evt.Cost.USD)
			}
		case adapter.EventRateLimit:
			if opts.verbose {
				fmt.Fprintf(os.Stderr, "[rate_limit] %s: %s (resets %s)\n", evt.Kitchen, evt.RateLimit.Status, evt.RateLimit.ResetsAt.Format("15:04"))
			}
			// Record rate limit in quota store
			if pdb != nil && evt.RateLimit != nil && evt.RateLimit.Status == "exhausted" {
				_ = pdb.Quotas().MarkExhausted(evt.Kitchen, evt.RateLimit.ResetsAt)
			}
		case adapter.EventDone:
			exitCode = evt.ExitCode
		case adapter.EventError:
			if opts.verbose {
				fmt.Fprintf(os.Stderr, "[error] %s: %s\n", evt.Kitchen, evt.Text)
			}
		}
	})
	if runErr != nil {
		return runErr
	}

	duration := time.Since(start).Seconds()
	_ = costInfo // used in verbose/json output below

	if opts.verbose {
		fmt.Fprintf(os.Stderr, "[dispatch] %s done (%.1fs, exit=%d)\n", lastKitchen, duration, exitCode)
	}

	if recordLegacyConversation {
		recordConversationDispatch(cfg, pdb, opts.prompt, lastKitchen, duration, exitCode, conv)
	}

	if opts.jsonOutput {
		out := map[string]any{
			"kitchen":    lastKitchen,
			"reason":     decision.Reason,
			"tier":       decision.Tier,
			"exit_code":  exitCode,
			"duration_s": duration,
			"output":     output.String(),
		}
		if costInfo != nil {
			out["cost_usd"] = costInfo.USD
		}
		if err := printJSON(out, true); err != nil {
			return err
		}
	}

	if exitCode != 0 {
		return &exitError{code: exitCode, err: fmt.Errorf("kitchen %s exited with code %d", decision.Kitchen, exitCode)}
	}

	return nil
}

// recordDispatch writes to PantryDB + ndjson audit trail + routing feedback.
func recordConversationDispatch(cfg *maitre.Config, pdb *pantry.DB, prompt, kitchenName string, duration float64, exitCode int, conv *conversation.Conversation) {
	taskType := sommelier.ClassifyTaskType(prompt)
	outcome := ledger.OutcomeFromExitCode(exitCode)

	if pdb != nil {
		if conv != nil && len(conv.Segments) > 0 {
			for _, ckpt := range conv.Checkpoints {
				if _, writeErr := pdb.Checkpoints().Insert(ckpt); writeErr != nil {
					fmt.Fprintf(os.Stderr, "[pantry] checkpoint warning: %v\n", writeErr)
				}
			}
			for i, seg := range conv.Segments {
				segDuration := duration
				if seg.EndedAt != nil {
					if d := seg.EndedAt.Sub(seg.StartedAt).Seconds(); d > 0 {
						segDuration = d
					}
				}
				segExitCode := 0
				segOutcome := "success"
				switch seg.Status {
				case conversation.SegmentExhausted:
					segExitCode = 1
					segOutcome = "failure"
				case conversation.SegmentFailed:
					segExitCode = 1
					segOutcome = "failure"
				}
				entry := pantry.LedgerEntry{
					Timestamp:      time.Now().UTC().Format(time.RFC3339),
					TaskHash:       ledger.HashPrompt(prompt),
					TaskType:       taskType,
					Kitchen:        seg.Provider,
					DurationSec:    segDuration,
					ExitCode:       segExitCode,
					Outcome:        segOutcome,
					DispatchMode:   "sync",
					ConversationID: conv.ID,
					SegmentID:      seg.ID,
					SegmentIndex:   i + 1,
					EndReason:      seg.EndReason,
				}
				if _, writeErr := pdb.Ledger().Insert(entry); writeErr != nil {
					fmt.Fprintf(os.Stderr, "[pantry] ledger warning: %v\n", writeErr)
				}
			}
		} else {
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

func makeConversationHydrator(pdb *pantry.DB, prompt string) orchestrator.ContextHydrator {
	return func(ctx context.Context, conv *conversation.Conversation) error {
		if conv == nil || pdb == nil {
			return nil
		}
		task := prompt
		if task == "" {
			task = conv.Prompt
		}
		service := conversation.RetrievalService{
			Plan: conversation.DefaultRetrievalPlan(),
			Backend: conversation.RetrievalBackend{
				FetchProcedural: func(ctx context.Context, _ string) ([]string, error) {
					items, _ := pdb.MemoryItems().ListActiveByType(conversation.MemoryProcedural, "project")
					items = append(items,
						"openspec/changes/milliways-provider-continuity/specs/provider-continuity/spec.md",
						"openspec/changes/milliways-provider-continuity/specs/exhaustion-detection/spec.md",
						"openspec/changes/milliways-provider-continuity/specs/memory-architecture/spec.md",
					)
					return items, nil
				},
				FetchSemantic: func(ctx context.Context, task string) (string, error) {
					if items, _ := pdb.MemoryItems().ListActiveByType(conversation.MemorySemantic, "project"); len(items) > 0 {
						return strings.Join(items, "\n"), nil
					}
					return fetchMemPalaceContext(ctx, task), nil
				},
				FetchRepo: func(ctx context.Context, task string) (string, error) {
					return fetchCodeGraphContext(ctx, task), nil
				},
			},
		}
		if _, err := service.Hydrate(ctx, conv, task); err != nil {
			return err
		}
		if conv.Memory.WorkingSummary == "" {
			conv.Memory.WorkingSummary = fmt.Sprintf("Continue the in-progress task %q from the preserved Milliways transcript and context.", task)
		}
		if conv.Memory.NextAction == "" {
			conv.Memory.NextAction = fmt.Sprintf("Continue executing %q from the current preserved state without restarting completed work.", task)
		}
		if invalidated, err := invalidateExpiredMemory(pdb); err == nil {
			conv.Context.InvalidatedMemoryCount = int(invalidated)
		}
		_ = promoteProceduralMemory(ctx, pdb, conv)
		_ = promoteSemanticMemory(ctx, pdb, conv)
		return nil
	}
}

func promoteProceduralMemory(ctx context.Context, pdb *pantry.DB, conv *conversation.Conversation) error {
	if conv == nil || pdb == nil || len(conv.Context.SpecRefs) == 0 {
		return nil
	}
	existing, _ := pdb.MemoryItems().ListActiveByType(conversation.MemoryProcedural, "project")
	cmd := os.Getenv("MILLIWAYS_MEMPALACE_MCP_CMD")
	var client *pantry.MemPalaceClient
	if cmd != "" {
		args := splitEnvArgs(os.Getenv("MILLIWAYS_MEMPALACE_MCP_ARGS"))
		var err error
		client, err = pantry.NewMemPalaceClient(cmd, args...)
		if err == nil {
			defer func() { _ = client.Close() }()
		}
	}

	now := time.Now()
	for _, ref := range conv.Context.SpecRefs {
		candidate := conversation.MemoryCandidate{
			SourceKind: "spec",
			MemoryType: conversation.MemoryProcedural,
			Text:       ref,
			Scope:      "project",
			Confidence: 1.0,
		}
		decision := conversation.EvaluateMemoryCandidate(candidate, existing, now)
		if !decision.Accept {
			continue
		}
		if _, err := pdb.MemoryItems().Insert(candidate, conv.ID); err == nil {
			existing = append(existing, ref)
		}
		if client != nil {
			_ = client.AddDrawer(ctx, pantry.AddDrawerRequest{
				Wing:       "milliways",
				Room:       "procedural-memory",
				Content:    ref,
				AddedBy:    "milliways",
				SourceFile: ref,
			})
		}
	}
	return nil
}

func promoteSemanticMemory(ctx context.Context, pdb *pantry.DB, conv *conversation.Conversation) error {
	if conv == nil || pdb == nil || conv.Context.MemPalaceText == "" {
		return nil
	}
	existing, _ := pdb.MemoryItems().ListActiveByType(conversation.MemorySemantic, "project")
	candidate := conversation.MemoryCandidate{
		SourceKind: "accepted_fact",
		MemoryType: conversation.MemorySemantic,
		Text:       conv.Context.MemPalaceText,
		Scope:      "project",
		Confidence: 0.9,
	}
	decision := conversation.EvaluateMemoryCandidate(candidate, existing, time.Now())
	if !decision.Accept {
		return nil
	}
	_, err := pdb.MemoryItems().Insert(candidate, conv.ID)
	return err
}

func invalidateExpiredMemory(pdb *pantry.DB) (int64, error) {
	if pdb == nil {
		return 0, nil
	}
	return pdb.MemoryItems().InvalidateExpired(time.Now())
}

func fetchCodeGraphContext(ctx context.Context, task string) string {
	cmd := os.Getenv("MILLIWAYS_CODEGRAPH_MCP_CMD")
	if cmd == "" {
		return "CodeGraph unavailable: set MILLIWAYS_CODEGRAPH_MCP_CMD to enable live context hydration."
	}
	args := splitEnvArgs(os.Getenv("MILLIWAYS_CODEGRAPH_MCP_ARGS"))
	client, err := pantry.NewCodeGraphClient(cmd, args...)
	if err != nil {
		return fmt.Sprintf("CodeGraph unavailable: %v", err)
	}
	defer func() { _ = client.Close() }()

	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	text, err := client.Context(cctx, task)
	if err != nil {
		return fmt.Sprintf("CodeGraph unavailable: %v", err)
	}
	return text
}

func fetchMemPalaceContext(ctx context.Context, task string) string {
	cmd := os.Getenv("MILLIWAYS_MEMPALACE_MCP_CMD")
	if cmd == "" {
		return "MemPalace unavailable: set MILLIWAYS_MEMPALACE_MCP_CMD to enable live memory hydration."
	}
	args := splitEnvArgs(os.Getenv("MILLIWAYS_MEMPALACE_MCP_ARGS"))
	client, err := pantry.NewMemPalaceClient(cmd, args...)
	if err != nil {
		return fmt.Sprintf("MemPalace unavailable: %v", err)
	}
	defer func() { _ = client.Close() }()

	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	drawers, err := client.Search(cctx, task, "", 3)
	if err != nil {
		return fmt.Sprintf("MemPalace unavailable: %v", err)
	}
	if len(drawers) == 0 {
		return "MemPalace recall: no relevant memories found."
	}
	var lines []string
	for _, drawer := range drawers {
		lines = append(lines, fmt.Sprintf("[%s/%s] %s", drawer.Wing, drawer.Room, drawer.Text))
	}
	return strings.Join(lines, "\n")
}

func splitEnvArgs(raw string) []string {
	if raw == "" {
		return nil
	}
	return strings.Fields(raw)
}

// readMMXAPIKey reads the API key from the mmx CLI config (~/.mmx/config.json).
// Returns "" if the file is absent or malformed.
func readMMXAPIKey() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".mmx", "config.json"))
	if err != nil {
		return ""
	}
	var cfg struct {
		APIKey string `json:"api_key"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return ""
	}
	return cfg.APIKey
}

// detectMempalaceMCP tries to find the mempalace MCP server command and args.
// It checks in order:
//  1. MILLIWAYS_MEMPALACE_MCP_CMD env var (if set)
//  2. python3 -m mempalace.mcp_server (system Python with mempalace installed)
//  3. ~/dev/src/pprojects/mempalace-milliways/.venv/bin/python -m mempalace.mcp_server
//
// Returns (cmd, args). If not found, returns ("", nil).
func detectMempalaceMCP(palacePath string) (string, []string) {
	// 1. Env var override.
	if cmd := os.Getenv("MILLIWAYS_MEMPALACE_MCP_CMD"); cmd != "" {
		return cmd, splitEnvArgs(os.Getenv("MILLIWAYS_MEMPALACE_MCP_ARGS"))
	}

	// 2. System python3 with mempalace package.
	if cmdPath, err := exec.LookPath("python3"); err == nil {
		args := []string{"-m", "mempalace.mcp_server"}
		if palacePath != "" {
			args = append(args, "--palace", palacePath)
		}
		return cmdPath, args
	}

	// 3. Known venv location.
	home, err := os.UserHomeDir()
	if err == nil {
		venvPython := filepath.Join(home, "dev/src/pprojects/mempalace-milliways/.venv/bin/python")
		if _, err := os.Stat(venvPython); err == nil {
			args := []string{"-m", "mempalace.mcp_server"}
			if palacePath != "" {
				args = append(args, "--palace", palacePath)
			}
			return venvPython, args
		}
	}

	return "", nil
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

func loadDispatchContextBundle(stdin io.Reader, contextStdin bool, contextJSON, contextFile string) (*editorcontext.Bundle, error) {
	if contextStdin {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return nil, fmt.Errorf("reading --context-stdin: %w", err)
		}
		bundle, err := editorcontext.ParseBundle(data)
		if err != nil {
			return nil, fmt.Errorf("parsing --context-stdin: %w", err)
		}
		return bundle, nil
	}

	if strings.TrimSpace(contextJSON) != "" {
		bundle, err := editorcontext.ParseBundle([]byte(contextJSON))
		if err != nil {
			return nil, fmt.Errorf("parsing --context-json: %w", err)
		}
		return bundle, nil
	}

	if strings.TrimSpace(contextFile) != "" {
		data, err := os.ReadFile(contextFile)
		if err != nil {
			return nil, fmt.Errorf("reading --context-file: %w", err)
		}
		bundle, err := editorcontext.ParseBundle(data)
		if err != nil {
			return nil, fmt.Errorf("parsing --context-file: %w", err)
		}
		return bundle, nil
	}

	return nil, nil
}

func assembleSignals(_ *maitre.Config, pdb *pantry.DB, prompt string, verbose bool, bundle *editorcontext.Bundle) *sommelier.Signals {
	if pdb == nil && bundle == nil {
		if verbose {
			fmt.Fprintf(os.Stderr, "[pantry] signals unavailable: no database\n")
		}
		return nil
	}

	signals := sommelier.NewSignals()

	if pdb == nil {
		if verbose {
			fmt.Fprintf(os.Stderr, "[pantry] signals unavailable: no database\n")
		}
	} else {
		taskType := sommelier.ClassifyTaskType(prompt)
		best, rate, err := pdb.Routing().BestKitchen(taskType, "", 5)
		if err == nil && best != "" {
			signals.LearnedKitchen = best
			signals.LearnedRate = rate
		}

		if verbose && signals.LearnedKitchen != "" {
			fmt.Fprintf(os.Stderr, "[pantry] learned: %s@%.0f%% for task_type=%s\n", signals.LearnedKitchen, signals.LearnedRate, taskType)
		}
	}

	mergeEditorSignals(signals, bundle)

	return signals
}

func mergeEditorSignals(signals *sommelier.Signals, bundle *editorcontext.Bundle) {
	if signals == nil || bundle == nil {
		return
	}

	editorSignals := bundle.Signals()
	if editorSignals.LSPErrors > 0 {
		signals.LSPErrors = editorSignals.LSPErrors
	}
	if editorSignals.LSPWarnings > 0 {
		signals.LSPWarnings = editorSignals.LSPWarnings
	}
	if editorSignals.Dirty {
		signals.Dirty = true
	}
	if editorSignals.InTestFile {
		signals.InTestFile = true
	}
	if editorSignals.Language != "" {
		signals.Language = editorSignals.Language
	}
	if editorSignals.FilesChanged > signals.FilesChanged {
		signals.FilesChanged = editorSignals.FilesChanged
	}
}

func openPantryDB() (*pantry.DB, error) {
	dbPath := filepath.Join(maitre.DefaultConfigDir(), "milliways.db")
	return pantry.Open(dbPath)
}

func openSubstrateClient() (*substrate.Client, error) {
	cmd := os.Getenv("MILLIWAYS_MEMPALACE_MCP_CMD")
	if cmd == "" {
		return nil, errors.New("opening substrate client: MILLIWAYS_MEMPALACE_MCP_CMD is not set")
	}
	client, err := substrate.New(cmd, splitEnvArgs(os.Getenv("MILLIWAYS_MEMPALACE_MCP_ARGS"))...)
	if err != nil {
		return nil, fmt.Errorf("opening substrate client: %w", err)
	}
	return client, nil
}

func openLegacyConversationDB(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("opening legacy conversation db: %w", err)
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func countLegacyConversationRows(db *sql.DB) (int, error) {
	if db == nil {
		return 0, nil
	}
	var checkpoints int
	if err := db.QueryRow(`SELECT COUNT(1) FROM mw_checkpoints`).Scan(&checkpoints); err != nil {
		return 0, fmt.Errorf("counting legacy checkpoints: %w", err)
	}
	var events int
	if err := db.QueryRow(`SELECT COUNT(1) FROM mw_runtime_events`).Scan(&events); err != nil {
		return 0, fmt.Errorf("counting legacy runtime events: %w", err)
	}
	return checkpoints + events, nil
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
		if kc.HTTPClient != nil {
			httpKitchen, err := adapter.NewHTTPKitchen(name, adapter.HTTPKitchenConfig{
				BaseURL:        kc.HTTPClient.BaseURL,
				AuthKey:        kc.HTTPClient.AuthKey,
				AuthType:       kc.HTTPClient.AuthType,
				Model:          kc.HTTPClient.Model,
				Stations:       kc.HTTPClient.Stations,
				Tier:           kitchen.ParseCostTier(kc.HTTPClient.Tier),
				ResponseFormat: kc.HTTPClient.ResponseFormat,
				Timeout:        time.Duration(kc.HTTPClient.Timeout) * time.Second,
			}, kc.Stations, kitchen.ParseCostTier(kc.CostTier))
			if err != nil {
				continue
			}
			reg.Register(httpKitchen)
			continue
		}

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

			printContinuityReport(pdb)

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
	fmt.Println("Note: tiered stats summarize completed dispatch outcomes. Continuity/failover support is reported separately below.")
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

func printContinuityReport(pdb *pantry.DB) {
	chains, err := pdb.Ledger().FailoverChains(5)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[report] continuity query failed: %v\n", err)
		return
	}
	if len(chains) == 0 {
		fmt.Println()
		fmt.Println("Continuity: no multi-provider conversations recorded yet")
		return
	}

	fmt.Println()
	fmt.Println("Continuity Chains")
	fmt.Println("═════════════════")
	fmt.Println("Conversation  Segments  Failovers  Providers")
	fmt.Println("────────────  ────────  ─────────  ─────────")
	for _, chain := range chains {
		label := chain.ConversationID
		if len(label) > 12 {
			label = label[:12]
		}
		fmt.Printf("%-12s %8d %10d  %s\n", label, chain.Segments, chain.Failovers, chain.Providers)
	}
}

func rateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rate <good|bad>",
		Short: "Rate the most recent dispatch as good or bad",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rating := args[0]
			if rating != "good" && rating != "bad" {
				return fmt.Errorf("rating must be 'good' or 'bad', got %q", rating)
			}

			pdb, err := openPantryDB()
			if err != nil {
				return fmt.Errorf("opening pantry: %w", err)
			}
			defer func() { _ = pdb.Close() }()

			entry, err := pdb.Ledger().Last()
			if err != nil {
				return fmt.Errorf("no ledger entries found — dispatch a task first")
			}

			outcome := "success"
			if rating == "bad" {
				outcome = "failure"
			}

			if err := pdb.Ledger().UpdateOutcome(entry.ID, outcome); err != nil {
				return err
			}

			// Also update routing feedback
			success := rating == "good"
			if routeErr := pdb.Routing().RecordOutcome(entry.TaskType, "", entry.Kitchen, success, entry.DurationSec); routeErr != nil {
				fmt.Fprintf(os.Stderr, "[rate] routing update warning: %v\n", routeErr)
			}

			prompt := entry.TaskHash
			if len(prompt) > 20 {
				prompt = prompt[:20] + "..."
			}
			fmt.Printf("Rated last dispatch (kitchen: %s, hash: %s) as %s\n", entry.Kitchen, prompt, rating)
			return nil
		},
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

func runREPL(configPath string, noRestore bool) error {
	cfg, err := maitre.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	var sc *substrate.Client
	mcpCmd := os.Getenv("MILLIWAYS_MEMPALACE_MCP_CMD")
	if mcpCmd != "" {
		sc, err = substrate.New(mcpCmd, splitEnvArgs(os.Getenv("MILLIWAYS_MEMPALACE_MCP_ARGS"))...)
		if err != nil {
			slog.Warn("could not connect to mempalace", "err", err)
			sc = nil
		}
	}

	logHandler := repl.NewReplLogHandler()
	slog.SetDefault(slog.New(logHandler))

	var r *repl.REPL
	if sc != nil {
		r = repl.NewREPLWithSubstrate(os.Stdout, sc)
		defer sc.Close()
	} else {
		r = repl.NewREPL(os.Stdout)
	}
	r.SetVersion(version)
	r.SetLogHandler(logHandler)

	pdb, pdbErr := openPantryDB()
	if pdbErr == nil {
		defer pdb.Close()
		kitchens := cfg.Kitchens
		r.SetQuotaFunc(func(name string) (*repl.QuotaInfo, error) {
			qi := &repl.QuotaInfo{}
			kc := kitchens[name]

			// Day
			dispatches, _ := pdb.Quotas().DailyDispatches(name)
			day := &repl.QuotaPeriod{Used: dispatches, Limit: kc.DailyLimit, Resets: "midnight UTC"}
			if kc.DailyLimit > 0 {
				day.Ratio = min(float64(dispatches)/float64(kc.DailyLimit), 1.0)
			}
			qi.Day = day

			// FiveHour
			fiveH, _ := pdb.Quotas().FiveHourDispatches(name)
			fh := &repl.QuotaPeriod{Used: fiveH, Limit: kc.FiveHourLimit}
			if kc.FiveHourLimit > 0 {
				fh.Ratio = min(float64(fiveH)/float64(kc.FiveHourLimit), 1.0)
			}
			qi.FiveHour = fh

			// Week
			weekly, _ := pdb.Quotas().WeeklyDispatches(name)
			wk := &repl.QuotaPeriod{Used: weekly, Limit: kc.WeeklyLimit}
			if kc.WeeklyLimit > 0 {
				wk.Ratio = min(float64(weekly)/float64(kc.WeeklyLimit), 1.0)
			}
			qi.Week = wk

			// Month
			monthly, _ := pdb.Quotas().MonthlyDispatches(name)
			mo := &repl.QuotaPeriod{Used: monthly, Limit: kc.MonthlyLimit}
			if kc.MonthlyLimit > 0 {
				mo.Ratio = min(float64(monthly)/float64(kc.MonthlyLimit), 1.0)
			}
			qi.Month = mo

			return qi, nil
		})
	}

	// Register claude runner
	r.Register("claude", repl.NewClaudeRunner())

	// Register codex runner
	r.Register("codex", repl.NewCodexRunner())

	// Register minimax runner
	var minimaxKey, minimaxModel, minimaxURL string
	if cfg.Kitchens["minimax"].HTTPClient != nil {
		// AuthKey is either a literal key or an env var name — resolve it.
		authKeyField := cfg.Kitchens["minimax"].HTTPClient.AuthKey
		if resolved := os.Getenv(authKeyField); resolved != "" {
			minimaxKey = resolved
		} else {
			// Fallback: read from ~/.mmx/config.json (mmx CLI config).
			minimaxKey = readMMXAPIKey()
		}
		minimaxModel = cfg.Kitchens["minimax"].HTTPClient.Model
		base := strings.TrimSuffix(cfg.Kitchens["minimax"].HTTPClient.BaseURL, "/text/chatcompletion_v2")
		base = strings.TrimSuffix(base, "/text")
		base = strings.TrimSuffix(base, "/")
		if base != "" {
			minimaxURL = base + "/text/chatcompletion_v2"
		}
	}
	r.Register("minimax", repl.NewMinimaxRunner(minimaxKey, minimaxModel, minimaxURL))

	// Register copilot runner
	r.Register("copilot", repl.NewCopilotRunner())

	r.SetNoRestore(noRestore)

	shell := repl.NewShell(os.Stdout, os.Stderr)
	shell.AddPane(repl.NewREPLPane(r, "repl"))
	return shell.Run(context.Background())
}

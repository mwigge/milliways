package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/mwigge/milliways/internal/conversation"
	asyncdispatch "github.com/mwigge/milliways/internal/dispatch"
	"github.com/mwigge/milliways/internal/kitchen"
	"github.com/mwigge/milliways/internal/kitchen/adapter"
	"github.com/mwigge/milliways/internal/ledger"
	"github.com/mwigge/milliways/internal/maitre"
	"github.com/mwigge/milliways/internal/observability"
	"github.com/mwigge/milliways/internal/orchestrator"
	"github.com/mwigge/milliways/internal/pantry"
	"github.com/mwigge/milliways/internal/project"
	"github.com/mwigge/milliways/internal/recipe"
	"github.com/mwigge/milliways/internal/sommelier"
	"github.com/mwigge/milliways/internal/tui"
	"github.com/spf13/cobra"
)

func runTUI(configPath string, tuiOpts tui.RunOpts) error {
	cfg, err := maitre.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	reg := buildRegistry(cfg)
	som := sommelier.New(cfg.Routing.Keywords, cfg.Routing.Default, cfg.Routing.BudgetFallback, reg)

	// Wire quota checking if pantry is available
	pdb, pdbErr := openPantryDB()
	if pdbErr == nil {
		defer func() { _ = pdb.Close() }()
		som.SetQuotaChecker(pdb.Quotas(), nil)
	}

	providerFactory := makeProviderFactory(cfg, reg, som, pdb, false)
	hydrator := makeConversationHydrator(pdb, "")
	sink := makeRuntimeSink(pdb)
	recorder := makeConversationRecorder(cfg, pdb)
	replayer := makeConversationReplayer(pdb)

	// Open ticket store for the jobs panel (best-effort — nil disables panel).
	var ticketStore *pantry.TicketStore
	if pdbErr == nil {
		ticketStore = pdb.Tickets()
	}
	tuiOpts.KitchenStates = buildKitchenStates(cfg, reg, pdb)

	// Detect project context for the TUI.
	if projectRoot := os.Getenv("MILLIWAYS_PROJECT_ROOT"); projectRoot != "" {
		fmt.Fprintf(os.Stderr, "[project] detecting from MILLIWAYS_PROJECT_ROOT=%q\n", projectRoot)
		if pc, err := project.ResolveProject(projectRoot); err == nil {
			palaceStr := "(nil)"
			if pc.PalacePath != nil {
				palaceStr = *pc.PalacePath
			}
			drawersStr := "(nil)"
			if pc.PalaceDrawers != nil {
				drawersStr = fmt.Sprintf("%d", *pc.PalaceDrawers)
			}
			fmt.Fprintf(os.Stderr, "[project] resolved: root=%s repo=%s palace=%s drawers=%s\n",
				pc.RepoRoot, pc.RepoName, palaceStr, drawersStr)
			ps := tui.ProjectState{
				RepoRoot: pc.RepoRoot,
				RepoName: pc.RepoName,
				PalacePath: func() string {
					if pc.PalacePath != nil {
						return *pc.PalacePath
					}
					return ""
				}(),
				PalaceDrawers: func() int {
					if pc.PalaceDrawers != nil {
						return *pc.PalaceDrawers
					}
					return 0
				}(),
				CodeGraphSymbols: pc.CodeGraphSymbols,
				AccessReadRule:   pc.AccessRules.Read,
				AccessWriteRule:  pc.AccessRules.Write,
			}
			tuiOpts.ProjectState = ps
		} else {
			fmt.Fprintf(os.Stderr, "[project] detection failed: %v\n", err)
		}
	} else {
		fmt.Fprintf(os.Stderr, "[project] MILLIWAYS_PROJECT_ROOT not set, skipping detection\n")
	}

	return tui.RunWithOpts(providerFactory, hydrator, sink, recorder, replayer, ticketStore, pdb, tuiOpts)
}

func buildKitchenStates(cfg *maitre.Config, reg *kitchen.Registry, pdb *pantry.DB) []tui.KitchenState {
	names := make([]string, 0, len(cfg.Kitchens))
	for name := range cfg.Kitchens {
		names = append(names, name)
	}
	sort.Strings(names)

	states := make([]tui.KitchenState, 0, len(names))
	for _, name := range names {
		state := tui.KitchenState{Name: name}
		k, ok := reg.Get(name)
		if !ok {
			state.Status = "not-installed"
			states = append(states, state)
			continue
		}
		state.Status = k.Status().String()
		if pdb != nil {
			if resetsAt, err := pdb.Quotas().ResetsAt(name); err == nil && !resetsAt.IsZero() && resetsAt.After(time.Now()) {
				state.Status = "exhausted"
				state.ResetsAt = resetsAt.In(time.Local).Format("15:04")
			} else if limit := cfg.Kitchens[name].DailyLimit; limit > 0 {
				if ratio, err := pdb.Quotas().UsageRatio(name, limit); err == nil && ratio >= cfg.Kitchens[name].EffectiveWarnThreshold() && ratio < 1.0 && state.Status == "ready" {
					state.Status = "warning"
					state.UsageRatio = ratio
				}
			}
		}
		states = append(states, state)
	}
	return states
}

func makeRuntimeSink(pdb *pantry.DB) observability.Sink {
	otelSink, _ := observability.NewOTelSink()
	if pdb == nil {
		return observability.MultiSink{otelSink}
	}
	runtimeSink := observability.FuncSink(func(evt observability.Event) {
		_, _ = pdb.RuntimeEvents().Insert(pantry.RuntimeEventRecord{
			ConversationID: evt.ConversationID,
			BlockID:        evt.BlockID,
			SegmentID:      evt.SegmentID,
			Kind:           evt.Kind,
			Provider:       evt.Provider,
			Text:           evt.Text,
			At:             evt.At.UTC().Format(time.RFC3339),
			Fields:         evt.Fields,
		})
	})
	ledgerSink := ledger.NewLedgerSink(pdb)
	return observability.MultiSink{runtimeSink, ledgerSink, otelSink}
}

func makeConversationRecorder(cfg *maitre.Config, pdb *pantry.DB) tui.ConversationRecorder {
	return func(prompt string, duration float64, exitCode int, conv *conversation.Conversation) {
		if conv == nil {
			return
		}
		lastKitchen := ""
		if len(conv.Segments) > 0 {
			lastKitchen = conv.Segments[len(conv.Segments)-1].Provider
		}
		recordConversationDispatch(cfg, pdb, prompt, lastKitchen, duration, exitCode, conv)
	}
}

func makeConversationReplayer(pdb *pantry.DB) tui.ConversationReplayer {
	if pdb == nil {
		return nil
	}
	return func(conversationID, blockID, prompt string, exitCode int) (*conversation.Conversation, []observability.Event, error) {
		if ckpt, err := pdb.Checkpoints().LatestByConversation(conversationID); err == nil && ckpt != nil {
			return pdb.RuntimeEvents().ReconstructConversationFromCheckpoint(ckpt, exitCode)
		}
		return pdb.RuntimeEvents().ReconstructConversation(conversationID, blockID, prompt, exitCode)
	}
}

func makeProviderFactory(cfg *maitre.Config, reg *kitchen.Registry, som *sommelier.Sommelier, pdb *pantry.DB, verbose bool) orchestrator.ProviderFactory {
	return func(ctx context.Context, prompt string, exclude map[string]bool, kitchenForce string, resumeSessionIDs map[string]string) (orchestrator.RouteResult, error) {
		decision := selectDecision(cfg, reg, som, pdb, prompt, kitchenForce, exclude)
		if decision.Kitchen == "" {
			return orchestrator.RouteResult{Decision: decision}, fmt.Errorf("no kitchens available")
		}

		k, ok := reg.Get(decision.Kitchen)
		if !ok || k.Status() != kitchen.Ready {
			return orchestrator.RouteResult{Decision: decision}, fmt.Errorf("kitchen %s not ready", decision.Kitchen)
		}

		resumeSessionID := ""
		if resumeSessionIDs != nil {
			resumeSessionID = resumeSessionIDs[decision.Kitchen]
		}
		adapt, err := adapter.AdapterFor(k, adapter.AdapterOpts{
			ResumeSessionID: resumeSessionID,
			Verbose:         verbose,
		})
		if err != nil {
			return orchestrator.RouteResult{Decision: decision}, fmt.Errorf("creating adapter for %s: %w", decision.Kitchen, err)
		}

		return orchestrator.RouteResult{
			Decision: decision,
			Adapter:  adapt,
		}, nil
	}
}

func selectDecision(cfg *maitre.Config, reg *kitchen.Registry, som *sommelier.Sommelier, pdb *pantry.DB, prompt, kitchenForce string, exclude map[string]bool) sommelier.Decision {
	if kitchenForce != "" && !exclude[kitchenForce] {
		return som.ForceRoute(kitchenForce)
	}

	var decision sommelier.Decision
	signals := assembleSignals(cfg, pdb, prompt, false)
	catalog := maitre.ScanSkills()
	var hint *sommelier.SkillHint
	if catalog.Total() > 0 {
		if kn, sk := catalog.HasSkill(prompt); sk != nil {
			hint = &sommelier.SkillHint{Kitchen: kn, SkillName: sk.Name}
		}
	}
	decision = som.RouteEnriched(prompt, signals, hint)

	if decision.Kitchen != "" && !exclude[decision.Kitchen] {
		if k, ok := reg.Get(decision.Kitchen); ok && k.Status() == kitchen.Ready {
			if len(exclude) > 0 {
				bestKitchen, bestCaps := bestContinuationKitchen(reg, exclude)
				if bestKitchen != "" {
					decisionCaps := capabilitiesForKitchen(reg, decision.Kitchen)
					if continuityScore(bestCaps) > continuityScore(decisionCaps) {
						return sommelier.Decision{
							Kitchen: bestKitchen,
							Reason:  fmt.Sprintf("continuation preferred %s for stronger continuity support", bestKitchen),
							Tier:    "continuation",
						}
					}
				}
			}
			return decision
		}
	}

	if len(exclude) > 0 {
		bestKitchen, _ := bestContinuationKitchen(reg, exclude)
		if bestKitchen != "" {
			return sommelier.Decision{
				Kitchen: bestKitchen,
				Reason:  fmt.Sprintf("continuation fallback → %s", bestKitchen),
				Tier:    "continuation",
			}
		}
	}

	for _, k := range reg.Ready() {
		if exclude[k.Name()] {
			continue
		}
		return sommelier.Decision{
			Kitchen: k.Name(),
			Reason:  fmt.Sprintf("continuation fallback → %s", k.Name()),
			Tier:    "fallback",
		}
	}

	return sommelier.Decision{
		Kitchen: "",
		Reason:  "no kitchens available",
		Tier:    "fallback",
	}
}

func bestContinuationKitchen(reg *kitchen.Registry, exclude map[string]bool) (string, adapter.Capabilities) {
	bestKitchen := ""
	bestCaps := adapter.Capabilities{}
	bestScore := -1
	for _, k := range reg.Ready() {
		if exclude[k.Name()] {
			continue
		}
		caps := capabilitiesForKitchen(reg, k.Name())
		score := continuityScore(caps)
		if score > bestScore {
			bestKitchen = k.Name()
			bestCaps = caps
			bestScore = score
		}
	}
	return bestKitchen, bestCaps
}

func capabilitiesForKitchen(reg *kitchen.Registry, kitchenName string) adapter.Capabilities {
	k, ok := reg.Get(kitchenName)
	if !ok {
		return adapter.Capabilities{}
	}
	adapt, err := adapter.AdapterFor(k, adapter.AdapterOpts{})
	if err != nil {
		return adapter.Capabilities{}
	}
	return adapt.Capabilities()
}

func continuityScore(caps adapter.Capabilities) int {
	score := 0
	if caps.NativeResume {
		score += 4
	}
	if caps.StructuredEvents {
		score += 3
	}
	if caps.ExhaustionDetection != "" && caps.ExhaustionDetection != "none" {
		score += 2
	}
	if caps.InteractiveSend {
		score++
	}
	return score
}

func dispatchRecipe(recipeName, prompt string, verbose bool, configPath string, keepContext bool) error {
	cfg, err := maitre.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	recipeSteps, ok := cfg.Recipes[recipeName]
	if !ok {
		return fmt.Errorf("unknown recipe %q — check carte.yaml", recipeName)
	}

	reg := buildRegistry(cfg)
	eng := recipe.NewEngine(reg, keepContext, recipe.StrategyStop)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	onLine := func(line string) { fmt.Println(line) }
	onCourse := func(i int, s recipe.Step, status string) {
		if verbose {
			fmt.Fprintf(os.Stderr, "[recipe] course %d/%d: %s via %s — %s\n", i+1, len(recipeSteps), s.Station, s.Kitchen, status)
		}
	}

	results, execErr := eng.Execute(ctx, recipeSteps, prompt, onLine, onCourse)

	// Log each course to pantry
	pdb, pdbErr := openPantryDB()
	if pdbErr == nil {
		defer func() { _ = pdb.Close() }()
		for _, r := range results {
			entry := pantry.LedgerEntry{
				Timestamp:    time.Now().UTC().Format(time.RFC3339),
				TaskHash:     ledger.HashPrompt(prompt),
				TaskType:     sommelier.ClassifyTaskType(prompt),
				Kitchen:      r.Step.Kitchen,
				Station:      r.Step.Station,
				DurationSec:  r.Duration.Seconds(),
				ExitCode:     r.Result.ExitCode,
				Outcome:      ledger.OutcomeFromExitCode(r.Result.ExitCode),
				DispatchMode: "recipe",
			}
			_, _ = pdb.Ledger().Insert(entry)
		}
	}

	if execErr != nil {
		return execErr
	}

	succeeded := 0
	for _, r := range results {
		if r.Error == nil && r.Result.ExitCode == 0 {
			succeeded++
		}
	}
	fmt.Fprintf(os.Stderr, "\nRecipe %q complete: %d/%d courses succeeded\n", recipeName, succeeded, len(recipeSteps))
	return nil
}

func dispatchAsync(prompt, kitchenForce string, verbose bool, configPath string) error {
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

	k, ok := reg.Get(decision.Kitchen)
	if !ok || k.Status() != kitchen.Ready {
		return fmt.Errorf("kitchen %q not available", decision.Kitchen)
	}

	pdb, err := openPantryDB()
	if err != nil {
		return fmt.Errorf("opening pantry: %w", err)
	}

	ad := asyncdispatch.NewAsyncDispatcher(pdb)
	ticketID, err := ad.DispatchAsync(context.Background(), k, prompt)
	if err != nil {
		_ = pdb.Close()
		return err
	}

	fmt.Printf("Dispatched: %s\nKitchen:    %s\nStatus:     running\nCheck:      milliways ticket %s\n", ticketID, decision.Kitchen, ticketID)

	ad.Wait()
	_ = pdb.Close()
	return nil
}

func dispatchDetach(_, _ string, _ bool, _ string) error {
	return fmt.Errorf("detached dispatch not yet implemented — use --async instead")
}

func ticketCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ticket <id>",
		Short: "Show status of an async/detached dispatch",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pdb, err := openPantryDB()
			if err != nil {
				return err
			}
			defer func() { _ = pdb.Close() }()

			ticket, err := pdb.Tickets().Get(args[0])
			if err != nil {
				return err
			}
			if ticket == nil {
				return fmt.Errorf("ticket %q not found", args[0])
			}

			fmt.Printf("Ticket:     %s\nKitchen:    %s\nMode:       %s\nStatus:     %s\nStarted:    %s\n",
				ticket.ID, ticket.Kitchen, ticket.Mode, ticket.Status, ticket.StartedAt)
			if ticket.CompletedAt != "" {
				fmt.Printf("Completed:  %s\n", ticket.CompletedAt)
			}
			if ticket.ExitCode != 0 {
				fmt.Printf("Exit Code:  %d\n", ticket.ExitCode)
			}
			return nil
		},
	}
}

func ticketsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tickets",
		Short: "List all async/detached dispatches",
		RunE: func(cmd *cobra.Command, args []string) error {
			pdb, err := openPantryDB()
			if err != nil {
				return err
			}
			defer func() { _ = pdb.Close() }()

			tickets, err := pdb.Tickets().List("")
			if err != nil {
				return err
			}
			if len(tickets) == 0 {
				fmt.Println("No tickets. Use --async or --detach to create one.")
				return nil
			}

			fmt.Println("ID            Kitchen      Mode      Status    Prompt")
			fmt.Println("──            ───────      ────      ──────    ──────")
			for _, t := range tickets {
				prompt := t.Prompt
				if len(prompt) > 40 {
					prompt = prompt[:40] + "..."
				}
				fmt.Printf("%-13s %-12s %-9s %-9s %s\n", t.ID, t.Kitchen, t.Mode, t.Status, prompt)
			}
			return nil
		},
	}
}

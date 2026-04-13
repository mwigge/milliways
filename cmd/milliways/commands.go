package main

import (
	"context"
	"fmt"
	"os"
	"time"

	asyncdispatch "github.com/mwigge/milliways/internal/dispatch"
	"github.com/mwigge/milliways/internal/kitchen"
	"github.com/mwigge/milliways/internal/ledger"
	"github.com/mwigge/milliways/internal/maitre"
	"github.com/mwigge/milliways/internal/pantry"
	"github.com/mwigge/milliways/internal/recipe"
	"github.com/mwigge/milliways/internal/sommelier"
	"github.com/mwigge/milliways/internal/tui"
	"github.com/spf13/cobra"
)

func runTUI(configPath string) error {
	cfg, err := maitre.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	reg := buildRegistry(cfg)
	som := sommelier.New(cfg.Routing.Keywords, cfg.Routing.Default, cfg.Routing.BudgetFallback, reg)

	dispatchFn := func(ctx context.Context, prompt, kitchenForce string) (kitchen.Result, sommelier.Decision, error) {
		var decision sommelier.Decision
		if kitchenForce != "" {
			decision = som.ForceRoute(kitchenForce)
		} else {
			signals := assembleSignals(cfg, prompt, false)
			catalog := maitre.ScanSkills()
			var hint *sommelier.SkillHint
			if catalog.Total() > 0 {
				if kn, sk := catalog.HasSkill(prompt); sk != nil {
					hint = &sommelier.SkillHint{Kitchen: kn, SkillName: sk.Name}
				}
			}
			decision = som.RouteEnriched(prompt, signals, hint)
		}

		if decision.Kitchen == "" {
			return kitchen.Result{}, decision, fmt.Errorf("no kitchens available")
		}

		k, ok := reg.Get(decision.Kitchen)
		if !ok || k.Status() != kitchen.Ready {
			return kitchen.Result{}, decision, fmt.Errorf("kitchen %s not ready", decision.Kitchen)
		}

		task := kitchen.Task{Prompt: prompt}
		result, err := k.Exec(ctx, task)
		return result, decision, err
	}

	return tui.Run(dispatchFn)
}

func dispatchRecipe(recipeName, prompt string, verbose bool, configPath string, keepContext bool) error {
	cfg, err := maitre.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	steps, ok := cfg.Recipes[recipeName]
	if !ok {
		return fmt.Errorf("unknown recipe %q — check carte.yaml", recipeName)
	}

	reg := buildRegistry(cfg)
	eng := recipe.NewEngine(reg, keepContext)

	recipeSteps := make([]recipe.Step, len(steps))
	for i, s := range steps {
		recipeSteps[i] = recipe.Step{Station: s.Station, Kitchen: s.Kitchen}
	}

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
				Outcome:      outcomeFromExit(r.Result.ExitCode),
				DispatchMode: "recipe",
			}
			_, _ = pdb.Ledger().Insert(entry)
		}
	}

	if execErr != nil {
		return execErr
	}
	fmt.Fprintf(os.Stderr, "\nRecipe %q complete: %d/%d courses succeeded\n", recipeName, len(results), len(recipeSteps))
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

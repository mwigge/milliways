package maitre

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/mwigge/milliways/internal/kitchen"
)

// KitchenHealth summarizes a kitchen's readiness.
type KitchenHealth struct {
	Name       string
	Status     kitchen.Status
	CostTier   kitchen.CostTier
	InstallCmd string
	AuthCmd    string
}

// Diagnose checks all kitchens and returns their health.
func Diagnose(reg *kitchen.Registry) []KitchenHealth {
	var results []KitchenHealth
	for name, k := range reg.All() {
		h := KitchenHealth{
			Name:     name,
			Status:   k.Status(),
			CostTier: k.CostTier(),
		}
		if s, ok := k.(kitchen.Setupable); ok {
			h.InstallCmd = s.InstallCmd()
			h.AuthCmd = s.AuthCmd()
		}
		results = append(results, h)
	}
	return results
}

// ReadyCounts returns (ready, total) kitchen counts.
func ReadyCounts(health []KitchenHealth) (int, int) {
	ready := 0
	for _, h := range health {
		if h.Status == kitchen.Ready {
			ready++
		}
	}
	return ready, len(health)
}

// PrintStatus renders the kitchen status table to stdout.
func PrintStatus(health []KitchenHealth) {
	fmt.Println("Kitchen      Status              Cost    Action")
	fmt.Println("───────      ──────              ────    ──────")

	for _, h := range health {
		action := ""
		switch h.Status {
		case kitchen.NotInstalled:
			action = h.InstallCmd
		case kitchen.NeedsAuth:
			action = h.AuthCmd
		case kitchen.Disabled:
			action = "(disabled in carte.yaml)"
		}

		fmt.Printf("%-12s %s %-18s %-7s %s\n",
			h.Name,
			h.Status.Symbol(),
			h.Status,
			h.CostTier,
			action,
		)
	}

	ready, total := ReadyCounts(health)
	fmt.Printf("\n%d/%d kitchens ready.", ready, total)
	if ready < total {
		fmt.Print(" Run 'milliways --setup <kitchen>' to add more.")
	}
	fmt.Println()
}

// SetupKitchen attempts to install and/or authenticate a kitchen.
// Returns nil on success, error on failure. The kitchen must implement
// Setupable; otherwise setup is not supported.
func SetupKitchen(k kitchen.Kitchen) error {
	s, ok := k.(kitchen.Setupable)
	if !ok {
		return fmt.Errorf("%s does not support setup", k.Name())
	}

	status := k.Status()

	switch status {
	case kitchen.Disabled:
		return fmt.Errorf("%s is disabled in carte.yaml — set enabled: true to use it", k.Name())
	case kitchen.Ready:
		fmt.Printf("✓ %s is already ready.\n", k.Name())
		return nil
	case kitchen.NotInstalled:
		fmt.Printf("Installing %s...\n", k.Name())
		installCmd := s.InstallCmd()
		if installCmd == "" {
			return fmt.Errorf("no install command configured for %s", k.Name())
		}
		parts := strings.Fields(installCmd)
		cmd := exec.Command(parts[0], parts[1:]...)
		cmd.Stdout = nil
		cmd.Stderr = nil
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("installing %s: %w\n  Try manually: %s", k.Name(), err, installCmd)
		}
		fmt.Printf("✓ %s installed.\n", k.Name())

		// Re-check — might need auth now
		if k.Status() == kitchen.Ready {
			fmt.Printf("✓ %s kitchen ready.\n", k.Name())
			return nil
		}
		fmt.Printf("  %s installed but needs authentication.\n", k.Name())
		fallthrough

	case kitchen.NeedsAuth:
		authCmd := s.AuthCmd()
		if authCmd == "" {
			return fmt.Errorf("no auth command configured for %s", k.Name())
		}
		fmt.Printf("Authenticate %s:\n  $ %s\n", k.Name(), authCmd)
		return fmt.Errorf("run the auth command above, then retry 'milliways status'")
	}

	return fmt.Errorf("unknown status for %s: %s", k.Name(), status)
}

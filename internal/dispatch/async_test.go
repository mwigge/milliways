package dispatch

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/kitchen"
	"github.com/mwigge/milliways/internal/pantry"
)

func openTestPantry(t *testing.T) *pantry.DB {
	t.Helper()
	pdb, err := pantry.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = pdb.Close() })
	return pdb
}

func TestDispatchAsync(t *testing.T) {
	t.Parallel()
	pdb := openTestPantry(t)
	d := NewAsyncDispatcher(pdb)

	k := kitchen.NewGeneric(kitchen.GenericConfig{Name: "echo-test", Cmd: "echo", Enabled: true})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ticketID, err := d.DispatchAsync(ctx, k, "hello async")
	if err != nil {
		t.Fatalf("DispatchAsync: %v", err)
	}
	if ticketID == "" {
		t.Fatal("expected non-empty ticket ID")
	}
	if len(ticketID) < 4 {
		t.Errorf("ticket ID too short: %q", ticketID)
	}

	// Wait for background goroutine to complete
	d.Wait()

	// Verify ticket was created and updated
	ticket, err := pdb.Tickets().Get(ticketID)
	if err != nil {
		t.Fatalf("Get ticket: %v", err)
	}
	if ticket == nil {
		t.Fatal("ticket not found")
	}
	if ticket.Status != "complete" {
		t.Errorf("expected status 'complete', got %q", ticket.Status)
	}
	if ticket.Kitchen != "echo-test" {
		t.Errorf("expected kitchen 'echo-test', got %q", ticket.Kitchen)
	}

	// Verify ledger entry was written
	total, _ := pdb.Ledger().Total()
	if total != 1 {
		t.Errorf("expected 1 ledger entry, got %d", total)
	}
}

func TestDispatchAsync_TicketList(t *testing.T) {
	t.Parallel()
	pdb := openTestPantry(t)
	d := NewAsyncDispatcher(pdb)

	k := kitchen.NewGeneric(kitchen.GenericConfig{Name: "echo-test", Cmd: "echo", Enabled: true})

	ctx := context.Background()
	_, _ = d.DispatchAsync(ctx, k, "task one")
	_, _ = d.DispatchAsync(ctx, k, "task two")

	d.Wait()

	tickets, err := pdb.Tickets().List("")
	if err != nil {
		t.Fatal(err)
	}
	if len(tickets) != 2 {
		t.Errorf("expected 2 tickets, got %d", len(tickets))
	}
}

func TestDispatchDetached(t *testing.T) {
	t.Parallel()
	pdb := openTestPantry(t)
	d := NewAsyncDispatcher(pdb)

	k := kitchen.NewGeneric(kitchen.GenericConfig{Name: "echo-test", Cmd: "echo", Enabled: true})

	ticketID, err := d.DispatchDetached(k, "detached task")
	if err != nil {
		t.Fatalf("DispatchDetached: %v", err)
	}
	if ticketID == "" {
		t.Fatal("expected non-empty ticket ID")
	}

	ticket, err := pdb.Tickets().Get(ticketID)
	if err != nil {
		t.Fatal(err)
	}
	if ticket == nil {
		t.Fatal("ticket not found")
	}
	if ticket.Mode != "detached" {
		t.Errorf("expected mode 'detached', got %q", ticket.Mode)
	}
}

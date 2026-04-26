package repl

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"

	"github.com/creack/pty"
	"golang.org/x/term"
)

func runPTY(cmd *exec.Cmd) (string, error) {
	return runPTYWithContext(cmd, nil)
}

func runPTYWithContext(cmd *exec.Cmd, ctx context.Context) (string, error) {
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return "", err
	}
	defer ptmx.Close()
	defer func() {
		if cmd.Process != nil {
			cmd.Process.Signal(syscall.SIGTERM)
			cmd.Wait()
		}
	}()

	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err == nil {
		defer term.Restore(int(os.Stdin.Fd()), oldState)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var captured bytes.Buffer

	wg.Add(2)

	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				os.Stdout.Write(buf[:n])
				mu.Lock()
				captured.Write(buf[:n])
				mu.Unlock()
			}
			if err != nil {
				break
			}
		}
	}()

	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				ptmx.Write(buf[:n])
			}
			if err != nil {
				ptmx.Close()
				break
			}
		}
	}()

	if ctx == nil {
		cmd.Wait()
		wg.Wait()
		mu.Lock()
		out := captured.String()
		mu.Unlock()
		return out, nil
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	errChan := make(chan error, 1)
	go func() {
		errChan <- cmd.Wait()
	}()

	select {
	case <-ctx.Done():
		if cmd.Process != nil {
			cmd.Process.Signal(syscall.SIGTERM)
		}
		wg.Wait()
		mu.Lock()
		out := captured.String()
		mu.Unlock()
		return out, ctx.Err()
	case err := <-errChan:
		wg.Wait()
		mu.Lock()
		out := captured.String()
		mu.Unlock()
		return out, err
	case sig := <-sigChan:
		if cmd.Process != nil {
			cmd.Process.Signal(sig)
		}
		wg.Wait()
		mu.Lock()
		out := captured.String()
		mu.Unlock()
		return out, ctx.Err()
	}
}
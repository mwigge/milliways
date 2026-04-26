package repl

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"

	"github.com/creack/pty"
	"golang.org/x/term"
)

var (
	noisePattern     = regexp.MustCompile(`^(?:[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]+Z ERROR|--.*|ERROR: Reconnecting)`)
	zscalerBlock    = regexp.MustCompile(`(?i)<!DOCTYPE|<html|<head|<meta name="description" content="Zscaler|<title>Internet Security by Zscaler`)
	htmlTagStripper = regexp.MustCompile(`<[^>]+>`)
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
	var inZscalerBlock bool
	var gotContent bool

	wg.Add(1)

	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				chunk := buf[:n]
				os.Stdout.Write(chunk)

				chunkStr := string(chunk)
				stripped := htmlTagStripper.ReplaceAllString(chunkStr, "")
				stripped = strings.TrimSpace(stripped)

				if stripped == "" {
					continue
				}
				if zscalerBlock.MatchString(chunkStr) {
					inZscalerBlock = true
					continue
				}
				if inZscalerBlock {
					if strings.Contains(strings.ToLower(chunkStr), "</html>") {
						inZscalerBlock = false
					}
					continue
				}
				if noisePattern.MatchString(chunkStr) {
					continue
				}

				mu.Lock()
				captured.WriteString(stripped + "\n")
				gotContent = true
				mu.Unlock()
			}
			if err != nil {
				break
			}
		}
	}()

	if ctx == nil {
		cmd.Wait()
		wg.Wait()
		mu.Lock()
		out := captured.String()
		if !gotContent {
			out = "[connection blocked by proxy]\n"
		}
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
		if !gotContent {
			out = "[connection blocked by proxy]\n"
		}
		mu.Unlock()
		return out, ctx.Err()
	case err := <-errChan:
		wg.Wait()
		mu.Lock()
		out := captured.String()
		if !gotContent {
			out = "[connection blocked by proxy]\n"
		}
		mu.Unlock()
		return out, err
	case sig := <-sigChan:
		if cmd.Process != nil {
			cmd.Process.Signal(sig)
		}
		wg.Wait()
		mu.Lock()
		out := captured.String()
		if !gotContent {
			out = "[connection blocked by proxy]\n"
		}
		mu.Unlock()
		return out, ctx.Err()
	}
}
package repl

import (
	"bufio"
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
	noisePattern = regexp.MustCompile(`^(?:[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]+Z ERROR|--.*|ERROR: Reconnecting)`)
	zscalerBlock = regexp.MustCompile(`(?i)<!DOCTYPE HTML|<html|<head|<meta name="description" content="Zscaler`)
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

	wg.Add(2)

	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(ptmx)
		for scanner.Scan() {
			line := scanner.Text()
			os.Stdout.Write([]byte(line + "\n"))

			if zscalerBlock.MatchString(line) {
				inZscalerBlock = true
			}
			if inZscalerBlock {
				if strings.HasSuffix(line, "</html>") || strings.HasSuffix(line, "</HTML>") {
					inZscalerBlock = false
				}
				continue
			}
			if noisePattern.MatchString(line) {
				continue
			}

			mu.Lock()
			captured.WriteString(line + "\n")
			gotContent = true
			mu.Unlock()
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
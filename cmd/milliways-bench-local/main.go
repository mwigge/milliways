package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/mwigge/milliways/internal/tieredbench"
)

func main() {
	var (
		executable  = flag.String("llama-cli", "", "path to llama-cli")
		model       = flag.String("model", "", "path to GGUF model")
		output      = flag.String("output", "", "qualification evidence JSON path")
		backend     = flag.String("backend", "rocm", "measured backend")
		hardware    = flag.String("hardware", "", "measured hardware class")
		gpuLayers   = flag.Int("gpu-layers", 99, "GPU layers")
		contextSize = flag.Int("context", 2048, "context tokens")
		maxTokens   = flag.Int("max-tokens", 128, "maximum generated tokens")
		memory      = flag.Uint64("memory-bytes", 0, "measured peak memory")
		timeout     = flag.Duration("timeout", 10*time.Minute, "suite timeout")
	)
	flag.Parse()
	if *executable == "" || *model == "" || *output == "" || *hardware == "" {
		fmt.Fprintln(os.Stderr, "-llama-cli, -model, -output, and -hardware are required")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	evidence := tieredbench.Run(ctx, tieredbench.LlamaCLIRunner{
		Executable: *executable, ModelPath: *model, GPULayers: *gpuLayers,
		ContextSize: *contextSize, MaxTokens: *maxTokens, MemoryBytes: *memory,
	}, tieredbench.Candidate{
		Model: "gemma4", Backend: *backend, HardwareClass: *hardware,
	}, tieredbench.DefaultSuite(), true)
	if err := tieredbench.WriteEvidence(*output, evidence); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	promotion := tieredbench.Qualify(evidence, tieredbench.Thresholds{
		MinimumQuality: 1, MaximumLatency: 30 * time.Second, RequiredPassRate: 1,
	}, *backend, *hardware)
	fmt.Printf("evidence=%s promoted=%t reason=%s\n", *output, promotion.Promoted, promotion.Reason)
}

package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/jesse0michael/evoke/internal/generate/comfyui"
	"github.com/kelseyhightower/envconfig"
)

// QueueCmd displays the current generation queue.
func QueueCmd(_ []string, _ bool) int {
	var cfg generateConfig
	if err := envconfig.Process("", &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "evoke queue: %v\n", err)
		return 1
	}

	gen := comfyui.New(cfg.ComfyURL)

	ctx := context.Background()
	running, pending, err := gen.Queue(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke queue: %v\n", err)
		return 1
	}

	if len(running) == 0 && len(pending) == 0 {
		fmt.Println("Queue is empty.")
		return 0
	}

	fmt.Printf("▶ running => %d\n", len(running))
	fmt.Printf("◦ pending => %d\n", len(pending))

	return 0
}

// ClearCmd clears the generation queue.
func ClearCmd(_ []string, _ bool) int {
	var cfg generateConfig
	if err := envconfig.Process("", &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "evoke clear: %v\n", err)
		return 1
	}

	gen := comfyui.New(cfg.ComfyURL)

	ctx := context.Background()
	if err := gen.ClearQueue(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "evoke clear: %v\n", err)
		return 1
	}

	fmt.Println("Queue cleared.")
	return 0
}

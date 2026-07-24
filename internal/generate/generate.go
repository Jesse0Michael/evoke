package generate

import (
	"context"

	evoke "github.com/jesse0michael/evoke/pkg/evoke"
)

type Result struct {
	Message  string
	Payload  string // The rendered workflow payload (populated when verbose/debug)
	PromptID string // Backend-assigned generation ID (e.g. ComfyUI prompt_id)
}

type Generator interface {
	Generate(ctx context.Context, composition *evoke.Composition) (*Result, error)
}

// QueueItem represents a single entry in the generation queue.
type QueueItem struct {
	PromptID string
	Number   int
	Inputs   string // Human-readable summary of what was submitted
}

// Output represents a generated output artifact.
type Output struct {
	Filename  string
	Subfolder string
	Type      string // e.g. "output", "temp"
}

// QueueViewer is implemented by backends that support queue introspection.
type QueueViewer interface {
	Queue(ctx context.Context) (running []QueueItem, pending []QueueItem, err error)
}

// QueueClearer is implemented by backends that support clearing the queue.
type QueueClearer interface {
	ClearQueue(ctx context.Context) error
}

// OutputResolver is implemented by backends that can resolve outputs for a given generation ID.
type OutputResolver interface {
	ResolveOutputs(ctx context.Context, promptID string) ([]Output, error)
}

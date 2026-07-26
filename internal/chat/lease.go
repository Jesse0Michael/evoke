package chat

import (
	"context"
	"net/url"
)

// Lease represents an acquired backend endpoint together with ownership of the
// process providing it. It is the seam between the chat session/transport and
// the backend's lifecycle: the session depends only on this interface, so how
// the backend is started, health-checked, and stopped can change without
// touching the conversation logic. In this version the only implementation is
// the managed llama.cpp process created by StartManaged.
type Lease interface {
	// Endpoint returns the OpenAI-compatible base URL of the backend (…/v1).
	Endpoint() *url.URL
	// Done is closed when the backend process exits on its own. It lets the
	// session react to a crash. After it is closed, Err reports the exit cause.
	Done() <-chan struct{}
	// Err returns the process exit error once Done is closed (nil on a clean
	// exit); it is nil while the process is still running.
	Err() error
	// Close stops the owned backend process. It is idempotent, attempts a
	// graceful shutdown first, then forces termination within the context's
	// deadline. It must never leave the process running.
	Close(ctx context.Context) error
}

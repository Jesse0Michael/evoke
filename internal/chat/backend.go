package chat

import (
	"context"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/jesse0michael/evoke/internal/process"
)

const shutdownGrace = 5 * time.Second

// Backend is the LLM server a chat session talks to, together with whatever
// ownership Evoke has over it. Launch starts a server and owns it; Connect
// attaches to one that was already running and owns nothing.
//
// The whole difference is one field: cmd is nil when Evoke did not start the
// process. Closing, crash detection, and log capture all follow from that,
// because none of them mean anything for a process someone else started.
type Backend struct {
	endpoint *url.URL

	cmd  *exec.Cmd           // nil when Evoke did not start the server
	logs *process.RingBuffer // nil when there is no child whose output to capture

	exited  chan struct{} // closed when the child exits; nil when there is no child
	waitErr error         // exit error, valid once exited is closed

	closeOnce sync.Once
	closeErr  error
}

// Endpoint is the OpenAI-compatible base URL of the server (…/v1).
func (b *Backend) Endpoint() *url.URL { return b.endpoint }

// Launched reports whether Evoke started this server, which is what decides
// whether ending the session stops anything.
func (b *Backend) Launched() bool { return b.cmd != nil }

// Done is closed if the server exits on its own, so a session can react to a
// crash. It is nil for a server Evoke did not start: there is no child to wait
// on, and one dying underneath the session surfaces as a failed request. A
// receive on a nil channel blocks forever, which is exactly the right behavior
// in the select that watches it.
func (b *Backend) Done() <-chan struct{} { return b.exited }

// Err returns the exit error once Done is closed, and nil while the server is
// still running or was never Evoke's to begin with.
func (b *Backend) Err() error {
	if b.exited == nil {
		return nil
	}
	select {
	case <-b.exited:
		return b.waitErr
	default:
		return nil
	}
}

// Close stops the server if Evoke started it: graceful termination first, then
// a forced kill of the process group if it does not exit within the context
// deadline. It is idempotent, and it does nothing for a server Evoke connected
// to, because stopping someone else's process is not Evoke's call.
func (b *Backend) Close(ctx context.Context) error {
	if b.cmd == nil {
		return nil
	}
	b.closeOnce.Do(func() { b.closeErr = b.shutdown(ctx) })
	return b.closeErr
}

func (b *Backend) shutdown(ctx context.Context) error {
	select {
	case <-b.exited:
		return nil // already gone
	default:
	}

	if err := process.Terminate(b.cmd); err != nil && !errors.Is(err, os.ErrProcessDone) {
		// Fall through to forced kill below.
		_ = err
	}

	graceCtx, cancel := context.WithTimeout(ctx, shutdownGrace)
	defer cancel()
	select {
	case <-b.exited:
		return nil
	case <-graceCtx.Done():
		_ = process.ForceKill(b.cmd)
		<-b.exited
		return nil
	}
}

// DrainLog returns server output captured since the last call and clears it, so
// verbose mode can print new lines between turns without repeating. It is empty
// for a server Evoke did not start, whose output goes wherever its owner sent it.
func (b *Backend) DrainLog() string {
	if b.logs == nil {
		return ""
	}
	return b.logs.Drain()
}

func (b *Backend) logTail() string {
	if b.logs == nil {
		return ""
	}
	tail := strings.TrimSpace(b.logs.String())
	if tail == "" {
		return ""
	}
	return "--- backend log (tail) ---\n" + tail
}

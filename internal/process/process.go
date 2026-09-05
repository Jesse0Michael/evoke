// Package process holds the primitives shared by every child process Evoke
// owns: putting it in its own group so the whole tree can be signalled, the
// graceful-then-forced termination pair, and a bounded log sink. The managed
// chat backend and the managed embedding server both launch a server, poll it
// for readiness, and must never leave it running — this is the part of that
// they have in common, kept in one place so a fix to the signalling reaches
// both.
package process

import "sync"

// RingBuffer is a byte sink that retains only the last Max bytes written, used
// to keep a bounded tail of a child's log output for diagnostics. It is safe
// for the concurrent writes os/exec performs for stdout and stderr.
type RingBuffer struct {
	mu  sync.Mutex
	buf []byte
	max int
}

// NewRingBuffer returns a buffer retaining the last max bytes written.
func NewRingBuffer(max int) *RingBuffer {
	return &RingBuffer{max: max}
}

func (r *RingBuffer) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, p...)
	if len(r.buf) > r.max {
		r.buf = r.buf[len(r.buf)-r.max:]
	}
	return len(p), nil
}

func (r *RingBuffer) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(r.buf)
}

// Drain returns the buffered content and clears it.
func (r *RingBuffer) Drain() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := string(r.buf)
	r.buf = r.buf[:0]
	return s
}

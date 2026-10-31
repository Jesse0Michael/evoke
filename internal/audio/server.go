package audio

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/jesse0michael/evoke/internal/process"
)

// DefaultServerExecutable is the resident TTS runtime a caller that synthesizes
// repeatedly — a voice-enabled chat session — launches once and reuses for
// every turn, rather than paying Generate's one-shot model load each time. It
// ships behind mlx-audio's "server" extra, which the base install Generate
// needs does not pull in:
//
//	uv tool install --force "mlx-audio[server]" --prerelease=allow --python 3.12
const DefaultServerExecutable = "mlx_audio.server"

// DefaultServerHost and DefaultServerPort are the loopback endpoint the server
// binds to. The port differs from evoke chat's own (8080), so a voice-enabled
// chat can run the LLM backend and this one side by side.
const (
	DefaultServerHost = "127.0.0.1"
	DefaultServerPort = 8090
)

// DefaultStartupTimeout bounds how long Launch waits for the server to answer
// before giving up, used when the caller passes zero.
const DefaultStartupTimeout = 180 * time.Second

const (
	serverShutdownGrace      = 5 * time.Second
	serverHealthPollInterval = 250 * time.Millisecond
	serverLogTailBytes       = 16 << 10
)

// ServerTrusted is the machine-local configuration for the resident server,
// parallel to Trusted for the one-shot runtime: a portable .evoke file never
// names a host, port, or binary.
type ServerTrusted struct {
	// Executable is the server binary (name on PATH or absolute path).
	Executable string
	Host       string
	Port       int
}

// Addr is the host:port the server binds to and is reached at.
func (t ServerTrusted) Addr() string {
	host := cmpOr(t.Host, DefaultServerHost)
	port := t.Port
	if port == 0 {
		port = DefaultServerPort
	}
	return net.JoinHostPort(host, fmt.Sprintf("%d", port))
}

// Backend is the resident server a voice-enabled chat talks to, together with
// whatever ownership Evoke has over it. The shape mirrors internal/chat.Backend:
// Launch starts the server and owns it, Connect attaches to one already running
// and owns nothing, and cmd is nil exactly when Evoke did not start the process.
type Backend struct {
	endpoint string // http://host:port, no trailing slash or path

	cmd     *exec.Cmd           // nil when Evoke did not start the server
	logs    *process.RingBuffer // nil when there is no child whose output to capture
	exited  chan struct{}       // closed when the child exits; nil when there is no child
	waitErr error               // exit error, valid once exited is closed

	closeOnce sync.Once
	closeErr  error
}

// Endpoint is the base URL the server answers requests on.
func (b *Backend) Endpoint() string { return b.endpoint }

// FindRunning reports whether something already answers at the configured
// endpoint. Unlike chat.FindRunning there is no model-identity probe: a wrong
// model loaded under a stray TTS request produces a wrong take that is simply
// discarded, not a transcript committed in the wrong character's voice, so the
// stakes here do not justify the same negotiation.
func FindRunning(t ServerTrusted) bool {
	return inUseAddr(t.Addr())
}

// Launch starts the server and waits for it to answer, or returns an error
// without leaving a process behind. Readiness is the server's root endpoint
// answering at all — like mlx_lm.server, it serves immediately and loads the
// requested model on the first real request, so "ready" here means the port is
// up, not that any model is in memory yet.
func Launch(ctx context.Context, t ServerTrusted, startupTimeout time.Duration) (*Backend, error) {
	if startupTimeout <= 0 {
		startupTimeout = DefaultStartupTimeout
	}
	exe := cmpOr(t.Executable, DefaultServerExecutable)
	path, err := exec.LookPath(exe)
	if err != nil {
		return nil, fmt.Errorf("%s not found on PATH: %s", exe, serverInstallHint)
	}

	addr := t.Addr()
	if inUseAddr(addr) {
		return nil, fmt.Errorf("refusing to start voice backend: %s is already in use (another voice backend running?); stop it or set a different EVOKE_AUDIO_PORT", addr)
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid voice backend address %q: %w", addr, err)
	}

	logs := process.NewRingBuffer(serverLogTailBytes)
	cmd := exec.Command(path, "--host", host, "--port", port) //nolint:gosec // args are fixed, not shell input
	cmd.Stdout = logs
	cmd.Stderr = logs
	process.SetGroup(cmd)

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start %s: %w", path, err)
	}

	b := &Backend{
		endpoint: "http://" + addr,
		cmd:      cmd,
		logs:     logs,
		exited:   make(chan struct{}),
	}
	go func() {
		b.waitErr = cmd.Wait()
		close(b.exited)
	}()

	if err := b.awaitReady(ctx, startupTimeout); err != nil {
		_ = b.Close(context.WithoutCancel(ctx))
		return nil, err
	}
	return b, nil
}

// Connect returns a Backend for a server that was already running. Evoke owns
// no process, so Close stops nothing.
func Connect(t ServerTrusted) *Backend {
	return &Backend{endpoint: "http://" + t.Addr()}
}

// awaitReady polls the server's root endpoint until it answers, the process
// exits early, the context is canceled, or the timeout elapses.
func (b *Backend) awaitReady(ctx context.Context, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(serverHealthPollInterval)
	defer tick.Stop()

	client := &http.Client{Timeout: 2 * time.Second}
	for {
		if healthy(ctx, client, b.endpoint+"/") {
			return nil
		}
		select {
		case <-b.exited:
			return fmt.Errorf("voice backend exited during startup: %v%s\n%s", b.waitErr, b.startupAdvice(), b.logTail())
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("voice backend did not become ready within %s\n%s", timeout, b.logTail())
		case <-tick.C:
		}
	}
}

// Close stops the server if Evoke started it: graceful termination first, then
// a forced kill of the process group if it does not exit within the shutdown
// grace period. It is idempotent, and does nothing for a server Evoke connected
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

	_ = process.Terminate(b.cmd)

	graceCtx, cancel := context.WithTimeout(ctx, serverShutdownGrace)
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

// serverInstallHint is how a missing server is installed. mlx-audio ships the
// server behind an extra, so the base install the one-shot runtime needs is not
// enough on its own.
const serverInstallHint = "install it with `uv tool install --force \"mlx-audio[server]\" --prerelease=allow --python 3.12`"

// startupAdvice turns the one startup failure that has a known remedy into
// that remedy. A LookPath check cannot catch it: mlx-audio installs the
// mlx_audio.server launcher whether or not the server extra's dependencies are
// present, so the executable always resolves and the process instead dies on
// an ImportError the moment it starts.
func (b *Backend) startupAdvice() string {
	if !strings.Contains(b.logTail(), "ModuleNotFoundError") {
		return ""
	}
	return "\nthe voice backend is installed but missing its dependencies: " + serverInstallHint
}

func (b *Backend) logTail() string {
	if b.logs == nil {
		return ""
	}
	tail := b.logs.String()
	if tail == "" {
		return ""
	}
	return "--- voice backend log (tail) ---\n" + tail
}

func healthy(ctx context.Context, client *http.Client, url string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func inUseAddr(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

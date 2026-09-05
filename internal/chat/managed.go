package chat

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"

	"github.com/jesse0michael/evoke/internal/process"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultStartupTimeout bounds how long StartManaged waits for the backend
	// to report healthy before giving up (large GGUF models load slowly).
	DefaultStartupTimeout = 180 * time.Second
	healthPollInterval    = 250 * time.Millisecond
	shutdownGrace         = 5 * time.Second
	logTailBytes          = 16 << 10
)

// StartManaged launches the backend described by the plan, waits for it to
// become healthy, and returns a Lease that owns the process. It validates the
// executable and model reference first, and refuses to start if the target port
// is already in use — Evoke runs the backend it owns and never adopts one it
// found, so a busy port is an error rather than an endpoint. On any startup
// failure it ensures no process is left running and includes the tail of the
// backend's log for diagnostics.
//
// Readiness is only as good as the backend's health endpoint. llama-server
// reports unhealthy while a model loads; mlx_lm.server answers 200 immediately
// and loads on first use, so for MLX a successful start means the port is
// serving, and the model load lands on the first turn.
func StartManaged(ctx context.Context, p *Plan, startupTimeout time.Duration) (Lease, error) {
	if startupTimeout <= 0 {
		startupTimeout = DefaultStartupTimeout
	}
	drv, ok := driverFor(p.Backend)
	if !ok {
		return nil, fmt.Errorf("unsupported chat backend %q", p.Backend)
	}
	if p.ModelPath == "" {
		return nil, fmt.Errorf("could not locate model %q; add its directory to chat.model_paths in settings", p.Model)
	}
	exe, err := exec.LookPath(p.Runtime.Executable)
	if err != nil {
		return nil, fmt.Errorf("backend executable %q not found: %w", p.Runtime.Executable, err)
	}
	if err := drv.validateModel(p); err != nil {
		return nil, err
	}

	addr := net.JoinHostPort(p.Runtime.Host, fmt.Sprintf("%d", p.Runtime.Port))
	if inUse(addr) {
		return nil, fmt.Errorf("refusing to start backend: %s is already in use (another backend running?); stop it or set a different chat.port in settings", addr)
	}

	logs := process.NewRingBuffer(logTailBytes)
	cmd := exec.Command(exe, p.commandArgs()...) //nolint:gosec // args are built from typed, validated settings, not shell input
	cmd.Stdout = logs
	cmd.Stderr = logs
	process.SetGroup(cmd)

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start %s: %w", exe, err)
	}

	endpoint, _ := url.Parse(fmt.Sprintf("http://%s/v1", addr))
	lease := &managedLease{cmd: cmd, endpoint: endpoint, logs: logs, exited: make(chan struct{})}
	go func() {
		lease.waitErr = cmd.Wait()
		close(lease.exited)
	}()

	if err := lease.awaitReady(ctx, fmt.Sprintf("http://%s/health", addr), startupTimeout); err != nil {
		// Ensure nothing is left running on any startup failure.
		_ = lease.Close(context.WithoutCancel(ctx))
		return nil, err
	}
	return lease, nil
}

// managedLease owns a backend child process that Evoke started.
type managedLease struct {
	cmd      *exec.Cmd
	endpoint *url.URL
	logs     *process.RingBuffer

	exited  chan struct{} // closed when the process exits
	waitErr error         // exit error, valid after exited is closed

	closeOnce sync.Once
	closeErr  error
}

func (l *managedLease) Endpoint() *url.URL    { return l.endpoint }
func (l *managedLease) Done() <-chan struct{} { return l.exited }

func (l *managedLease) Err() error {
	select {
	case <-l.exited:
		return l.waitErr
	default:
		return nil
	}
}

// awaitReady polls the health endpoint until the backend responds, the process
// exits early, the context is canceled, or the startup timeout elapses.
func (l *managedLease) awaitReady(ctx context.Context, healthURL string, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(healthPollInterval)
	defer tick.Stop()

	client := &http.Client{Timeout: 2 * time.Second}
	for {
		if healthy(ctx, client, healthURL) {
			return nil
		}
		select {
		case <-l.exited:
			return fmt.Errorf("backend exited during startup: %v\n%s", l.waitErr, l.logTail())
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("backend did not become ready within %s\n%s", timeout, l.logTail())
		case <-tick.C:
		}
	}
}

// Close stops the backend: graceful termination first, then a forced kill if it
// does not exit within the context deadline (or shutdownGrace). Idempotent.
func (l *managedLease) Close(ctx context.Context) error {
	l.closeOnce.Do(func() { l.closeErr = l.shutdown(ctx) })
	return l.closeErr
}

func (l *managedLease) shutdown(ctx context.Context) error {
	select {
	case <-l.exited:
		return nil // already gone
	default:
	}

	if err := process.Terminate(l.cmd); err != nil && !errors.Is(err, os.ErrProcessDone) {
		// Fall through to forced kill below.
		_ = err
	}

	graceCtx, cancel := context.WithTimeout(ctx, shutdownGrace)
	defer cancel()
	select {
	case <-l.exited:
		return nil
	case <-graceCtx.Done():
		_ = process.ForceKill(l.cmd)
		<-l.exited
		return nil
	}
}

func (l *managedLease) logTail() string {
	tail := strings.TrimSpace(l.logs.String())
	if tail == "" {
		return ""
	}
	return "--- backend log (tail) ---\n" + tail
}

// DrainLog returns backend log output captured since the last call and clears
// it, so verbose mode can print new lines between turns without repeating.
func (l *managedLease) DrainLog() string {
	return l.logs.Drain()
}

// healthy reports whether the backend answers the health endpoint with 2xx.
func healthy(ctx context.Context, client *http.Client, healthURL string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
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

// inUse reports whether something is already listening on addr.
func inUse(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

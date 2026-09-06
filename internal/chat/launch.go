package chat

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"time"

	"github.com/jesse0michael/evoke/internal/process"
)

const (
	// DefaultStartupTimeout bounds how long Launch waits for a server to report
	// healthy before giving up (large GGUF models load slowly).
	DefaultStartupTimeout = 180 * time.Second
	healthPollInterval    = 250 * time.Millisecond
	logTailBytes          = 16 << 10
)

// Launch starts the server described by the plan, waits for it to become
// healthy, and returns a Backend that owns the process. It validates the
// executable and model reference first, and on any startup failure it ensures
// no process is left running and includes the tail of the server's log.
//
// Readiness is only as good as the server's health endpoint. llama-server
// reports unhealthy while a model loads; mlx_lm.server answers 200 immediately
// and loads on first use, so for MLX a successful start means the port is
// serving and the model load lands on the first turn.
//
// The caller is expected to have checked the endpoint with FindRunning first;
// the in-use check here is a race guard, not the decision.
func Launch(ctx context.Context, p *Plan, startupTimeout time.Duration) (*Backend, error) {
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

	addr := endpointAddr(p)
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

	b := &Backend{
		endpoint: baseURL(addr),
		cmd:      cmd,
		logs:     logs,
		exited:   make(chan struct{}),
	}
	go func() {
		b.waitErr = cmd.Wait()
		close(b.exited)
	}()

	if err := b.awaitReady(ctx, fmt.Sprintf("http://%s/health", addr), startupTimeout); err != nil {
		// Ensure nothing is left running on any startup failure.
		_ = b.Close(context.WithoutCancel(ctx))
		return nil, err
	}
	return b, nil
}

// awaitReady polls the health endpoint until the server responds, the process
// exits early, the context is canceled, or the startup timeout elapses.
func (b *Backend) awaitReady(ctx context.Context, healthURL string, timeout time.Duration) error {
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
		case <-b.exited:
			return fmt.Errorf("backend exited during startup: %v\n%s", b.waitErr, b.logTail())
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("backend did not become ready within %s\n%s", timeout, b.logTail())
		case <-tick.C:
		}
	}
}

// endpointAddr is the host:port a plan's server listens on.
func endpointAddr(p *Plan) string {
	return net.JoinHostPort(p.Runtime.Host, fmt.Sprintf("%d", p.Runtime.Port))
}

// baseURL is the OpenAI-compatible base for an address.
func baseURL(addr string) *url.URL {
	u, _ := url.Parse(fmt.Sprintf("http://%s/v1", addr))
	return u
}

// healthy reports whether the server answers the health endpoint with 2xx.
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

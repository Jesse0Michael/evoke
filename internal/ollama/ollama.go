// Package ollama makes the embedding endpoint that KNOWLEDGE retrieval depends
// on available, starting a local `ollama serve` when nothing is answering yet.
//
// This is deliberately not the ownership model `internal/chat` uses for the
// language backend. There, a busy port is an error, because settings a request
// cannot carry (--ctx-size, -ngl) are only true of a process Evoke launched
// itself. Ollama's API names the model on every request, so a server that was
// already running is indistinguishable from one started here — adopting it is
// correct, and it is the common case on a machine where Ollama runs as a
// service. Evoke only ever stops a server it started.
package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jesse0michael/evoke/internal/process"
)

const (
	// DefaultStartupTimeout bounds the wait for a launched server to answer.
	// Ollama serves its API before loading any model, so this covers process
	// startup only, not a model load.
	DefaultStartupTimeout = 30 * time.Second
	// Executable is the binary launched when the endpoint is not answering.
	Executable = "ollama"

	pollInterval = 200 * time.Millisecond
	logTailBytes = 8 << 10
)

// Server is a reachable ollama endpoint. When Evoke started the process, the
// Server owns it and Close stops it; when an already-running server was
// adopted, Close does nothing.
type Server struct {
	started bool
	cmd     *exec.Cmd
	logs    *process.RingBuffer

	exited  chan struct{}
	waitErr error

	closeOnce sync.Once
	closeErr  error
}

// Started reports whether Evoke launched the server, as opposed to adopting one
// that was already running.
func (s *Server) Started() bool { return s.started }

// Ensure returns a reachable ollama endpoint at baseURL serving every named
// model, launching `ollama serve` if nothing answers there. The caller must
// Close the result.
//
// A server is launched only for a loopback baseURL: a configured remote
// endpoint that is down is an error, since starting a local server in its place
// would quietly embed against something other than what was configured.
func Ensure(ctx context.Context, baseURL string, models []string, timeout time.Duration) (*Server, error) {
	if timeout <= 0 {
		timeout = DefaultStartupTimeout
	}
	addr, err := address(baseURL)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 5 * time.Second}
	base := strings.TrimRight(baseURL, "/")

	srv := &Server{}
	if !answering(ctx, client, base) {
		if !isLoopback(addr) {
			return nil, fmt.Errorf("embedding endpoint %s is not reachable; start it, or point chat.embed_url at one that is running", baseURL)
		}
		if srv, err = launch(ctx, addr, base, client, timeout); err != nil {
			return nil, err
		}
	}

	if err := srv.checkModels(ctx, client, base, baseURL, models); err != nil {
		_ = srv.Close(context.WithoutCancel(ctx))
		return nil, err
	}
	return srv, nil
}

// launch starts `ollama serve` bound to addr and waits for it to answer.
func launch(ctx context.Context, addr, base string, client *http.Client, timeout time.Duration) (*Server, error) {
	exe, err := exec.LookPath(Executable)
	if err != nil {
		return nil, fmt.Errorf("nothing is serving embeddings at %s and %q is not installed; install Ollama or point chat.embed_url at a running endpoint", addr, Executable)
	}

	logs := process.NewRingBuffer(logTailBytes)
	cmd := exec.Command(exe, "serve") //nolint:gosec // exe is resolved from PATH, no user input
	// OLLAMA_HOST is how ollama serve chooses its listen address; it must match
	// the endpoint the embedder will call, which may not be the default port.
	cmd.Env = append(os.Environ(), "OLLAMA_HOST="+addr)
	cmd.Stdout = logs
	cmd.Stderr = logs
	process.SetGroup(cmd)

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start %s serve: %w", exe, err)
	}

	srv := &Server{started: true, cmd: cmd, logs: logs, exited: make(chan struct{})}
	go func() {
		srv.waitErr = cmd.Wait()
		close(srv.exited)
	}()

	if err := srv.awaitReady(ctx, client, base, timeout); err != nil {
		_ = srv.Close(context.WithoutCancel(ctx))
		return nil, err
	}
	return srv, nil
}

func (s *Server) awaitReady(ctx context.Context, client *http.Client, base string, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(pollInterval)
	defer tick.Stop()

	for {
		if answering(ctx, client, base) {
			return nil
		}
		select {
		case <-s.exited:
			return fmt.Errorf("ollama serve exited during startup: %v\n%s", s.waitErr, s.logTail())
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("ollama serve did not answer within %s\n%s", timeout, s.logTail())
		case <-tick.C:
		}
	}
}

// checkModels fails when a required model is not present locally. Pulling one is
// a several-hundred-megabyte download, so it stays an explicit act: the error
// carries the exact command rather than performing it.
func (s *Server) checkModels(ctx context.Context, client *http.Client, base, baseURL string, models []string) error {
	models = slices.Compact(slices.Sorted(slices.Values(models)))
	if len(models) == 0 {
		return nil
	}

	installed, err := tags(ctx, client, base)
	if err != nil {
		return err
	}

	var missing []string
	for _, m := range models {
		if m != "" && !slices.Contains(installed, m) && !slices.Contains(installed, m+":latest") {
			missing = append(missing, m)
		}
	}
	if len(missing) == 0 {
		return nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "embedding model %s not available at %s; pull it with:\n",
		strings.Join(quoteAll(missing), ", "), baseURL)
	for _, m := range missing {
		fmt.Fprintf(&b, "\n    %s pull %s", Executable, m)
	}
	return errors.New(b.String())
}

// Close stops the server if Evoke started it: graceful termination first, then a
// forced kill if it does not exit in time. Idempotent, and a no-op for an
// adopted server.
func (s *Server) Close(ctx context.Context) error {
	if s == nil || !s.started {
		return nil
	}
	s.closeOnce.Do(func() { s.closeErr = s.shutdown(ctx) })
	return s.closeErr
}

func (s *Server) shutdown(ctx context.Context) error {
	select {
	case <-s.exited:
		return nil
	default:
	}

	if err := process.Terminate(s.cmd); err != nil && !errors.Is(err, os.ErrProcessDone) {
		_ = err // fall through to the forced kill below
	}

	graceCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	select {
	case <-s.exited:
		return nil
	case <-graceCtx.Done():
		_ = process.ForceKill(s.cmd)
		<-s.exited
		return nil
	}
}

func (s *Server) logTail() string {
	if s.logs == nil {
		return ""
	}
	tail := strings.TrimSpace(s.logs.String())
	if tail == "" {
		return ""
	}
	return "--- ollama log (tail) ---\n" + tail
}

// answering reports whether an ollama API is serving at base. /api/tags is the
// probe rather than the root path because it proves the API itself responds,
// and it is the same call the model check needs.
func answering(ctx context.Context, client *http.Client, base string) bool {
	_, err := tags(ctx, client, base)
	return err == nil
}

// tags returns the names of the models installed on the server.
func tags(ctx context.Context, client *http.Client, base string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/tags", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build tags request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to reach %s: %w", base, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s/api/tags returned status %d", base, resp.StatusCode)
	}

	var parsed struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("failed to decode model list: %w", err)
	}
	names := make([]string, 0, len(parsed.Models))
	for _, m := range parsed.Models {
		names = append(names, m.Name)
	}
	return names, nil
}

// address returns the host:port an ollama server would bind to serve baseURL.
func address(baseURL string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("invalid embed URL %q", baseURL)
	}
	if u.Port() != "" {
		return u.Host, nil
	}
	port := "80"
	if u.Scheme == "https" {
		port = "443"
	}
	return net.JoinHostPort(u.Hostname(), port), nil
}

func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func quoteAll(vals []string) []string {
	out := make([]string, len(vals))
	for i, v := range vals {
		out[i] = fmt.Sprintf("%q", v)
	}
	return out
}

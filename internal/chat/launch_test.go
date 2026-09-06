package chat

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestMain lets the test binary impersonate a llama-server child process so the
// launched-process lifecycle can be exercised without a real model. When the
// FAKE_LLAMA env var is set, the process behaves like the fake and never runs
// the test suite. Launch is pointed at os.Args[0] with FAKE_LLAMA set.
func TestMain(m *testing.M) {
	switch os.Getenv("FAKE_LLAMA") {
	case "ready":
		runFakeReady()
	case "exit":
		os.Exit(3)
	}
	os.Exit(m.Run())
}

// runFakeReady serves a health endpoint on the --port passed by Launch and
// blocks until terminated, mimicking a healthy backend.
func runFakeReady() {
	port := ""
	for i, a := range os.Args {
		if a == "--port" && i+1 < len(os.Args) {
			port = os.Args[i+1]
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	srv := &http.Server{Addr: net.JoinHostPort("127.0.0.1", port), Handler: mux} //nolint:gosec // test fake

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGTERM, os.Interrupt)
		<-sig
		os.Exit(0)
	}()
	_ = srv.ListenAndServe()
	os.Exit(0)
}

// freePort returns a currently-free TCP port on loopback.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

func fakeModel(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "model.gguf")
	require.NoError(t, os.WriteFile(path, []byte("fake"), 0o600))
	return path
}

func managedPlan(t *testing.T, port int) *Plan {
	t.Helper()
	return &Plan{
		Backend:   backendLlamaCpp,
		Model:     "test-model",
		ModelPath: fakeModel(t),
		Runtime: RuntimeSpec{
			Executable:    os.Args[0],
			Host:          "127.0.0.1",
			Port:          port,
			ContextWindow: 8192,
			GPULayers:     0,
		},
	}
}

func TestLaunchLifecycle(t *testing.T) {
	t.Setenv("FAKE_LLAMA", "ready")
	plan := managedPlan(t, freePort(t))

	backend, err := Launch(t.Context(), plan, 10*time.Second)
	require.NoError(t, err)

	require.Equal(t, "http://127.0.0.1:"+strconv.Itoa(plan.Runtime.Port)+"/v1", backend.Endpoint().String())
	require.Nil(t, backend.Err(), "backend should be running")

	// Close stops the owned process; it must actually exit (no orphan).
	require.NoError(t, backend.Close(context.Background()))
	select {
	case <-backend.Done():
	default:
		t.Fatal("backend process was not terminated by Close")
	}

	// Close is idempotent.
	require.NoError(t, backend.Close(context.Background()))
}

func TestLaunchEarlyExit(t *testing.T) {
	t.Setenv("FAKE_LLAMA", "exit")
	plan := managedPlan(t, freePort(t))

	_, err := Launch(t.Context(), plan, 5*time.Second)

	require.Error(t, err)
	require.Contains(t, err.Error(), "exited during startup")
}

func TestLaunchPortInUse(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = l.Close() }()
	port := l.Addr().(*net.TCPAddr).Port

	// FAKE_LLAMA is deliberately unset: the port check must fail before any
	// process is spawned.
	plan := managedPlan(t, port)

	_, err = Launch(t.Context(), plan, 5*time.Second)

	require.Error(t, err)
	require.Contains(t, err.Error(), "already in use")
}

func TestLaunchMissingModel(t *testing.T) {
	plan := managedPlan(t, freePort(t))
	plan.ModelPath = filepath.Join(t.TempDir(), "does-not-exist.gguf")

	_, err := Launch(t.Context(), plan, 5*time.Second)

	require.Error(t, err)
	require.Contains(t, err.Error(), "model file not found")
}

func TestLaunchUnresolvedModel(t *testing.T) {
	plan := managedPlan(t, freePort(t))
	plan.ModelPath = ""

	_, err := Launch(t.Context(), plan, 5*time.Second)

	require.Error(t, err)
	require.Contains(t, err.Error(), "could not locate model")
}

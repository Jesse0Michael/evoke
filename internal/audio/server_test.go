package audio

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestMain lets the test binary impersonate an mlx_audio.server child process
// so the launched-process lifecycle can be exercised without the real runtime
// installed. When FAKE_MLX_AUDIO is set, the process behaves like the fake and
// never runs the test suite. Launch is pointed at os.Args[0] with it set,
// mirroring internal/chat's TestLaunchLifecycle.
func TestMain(m *testing.M) {
	switch os.Getenv("FAKE_MLX_AUDIO") {
	case "ready":
		runFakeReady()
	case "exit":
		os.Exit(3)
	}
	os.Exit(m.Run())
}

// runFakeReady serves the root endpoint on the --port passed by Launch and
// blocks until terminated, mimicking a healthy server.
func runFakeReady() {
	port := ""
	for i, a := range os.Args {
		if a == "--port" && i+1 < len(os.Args) {
			port = os.Args[i+1]
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
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

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

func TestLaunchLifecycle(t *testing.T) {
	t.Setenv("FAKE_MLX_AUDIO", "ready")
	trusted := ServerTrusted{Executable: os.Args[0], Host: "127.0.0.1", Port: freePort(t)}

	backend, err := Launch(t.Context(), trusted, 10*time.Second)
	require.NoError(t, err)
	require.Equal(t, "http://"+trusted.Addr(), backend.Endpoint())

	require.NoError(t, backend.Close(context.Background()))

	// Close is idempotent.
	require.NoError(t, backend.Close(context.Background()))
}

func TestLaunchEarlyExit(t *testing.T) {
	t.Setenv("FAKE_MLX_AUDIO", "exit")
	trusted := ServerTrusted{Executable: os.Args[0], Host: "127.0.0.1", Port: freePort(t)}

	_, err := Launch(t.Context(), trusted, 5*time.Second)

	require.Error(t, err)
	require.Contains(t, err.Error(), "exited during startup")
}

func TestLaunchPortInUse(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = l.Close() }()
	port := l.Addr().(*net.TCPAddr).Port

	// FAKE_MLX_AUDIO is deliberately unset: the port check must fail before any
	// process is spawned.
	trusted := ServerTrusted{Executable: os.Args[0], Host: "127.0.0.1", Port: port}

	_, err = Launch(t.Context(), trusted, 5*time.Second)

	require.Error(t, err)
	require.Contains(t, err.Error(), "already in use")
}

func TestFindRunningAndConnect(t *testing.T) {
	t.Run("nothing listening", func(t *testing.T) {
		trusted := ServerTrusted{Host: "127.0.0.1", Port: freePort(t)}
		require.False(t, FindRunning(trusted))
	})

	t.Run("something listening", func(t *testing.T) {
		t.Setenv("FAKE_MLX_AUDIO", "ready")
		trusted := ServerTrusted{Executable: os.Args[0], Host: "127.0.0.1", Port: freePort(t)}
		backend, err := Launch(t.Context(), trusted, 10*time.Second)
		require.NoError(t, err)
		defer func() { _ = backend.Close(context.Background()) }()

		require.True(t, FindRunning(trusted))

		connected := Connect(trusted)
		require.Equal(t, backend.Endpoint(), connected.Endpoint())
		// Connect owns nothing: closing it must not stop the process Launch owns.
		require.NoError(t, connected.Close(context.Background()))
		require.True(t, FindRunning(trusted), "Connect's Close must not have stopped the backend it did not launch")
	})
}

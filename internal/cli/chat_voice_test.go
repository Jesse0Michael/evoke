package cli

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/jesse0michael/evoke/internal/audio"
	"github.com/jesse0michael/evoke/internal/chat"
	evoke "github.com/jesse0michael/evoke/pkg/evoke"
	"github.com/stretchr/testify/require"
)

func TestIsVoiceCommand(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want bool
	}{
		{name: "bare command", msg: "/voice", want: true},
		{name: "unlike /image, arguments are not the command", msg: "/voice on", want: false},
		{name: "unrelated command", msg: "/image", want: false},
		{name: "plain text", msg: "hello", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, isVoiceCommand(tt.msg))
		})
	}
}

func TestChatVoiceLabel(t *testing.T) {
	st := chatStyle{on: false} // styling off keeps the rendered text comparable
	tests := []struct {
		name    string
		enabled bool
	}{
		{name: "off", enabled: false},
		{name: "on", enabled: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := chatVoice{enabled: tt.enabled}
			require.Equal(t, "/voice", c.label(st))
		})
	}
}

// voiceComposition builds a minimal composition with a VOICE description, the
// smallest input audio.Compile accepts.
func voiceComposition() *evoke.Composition {
	return &evoke.Composition{Voice: evoke.Prompt{Positive: []string{"low alto", "unhurried"}}}
}

// listenRoot starts a server answering the root endpoint the way Launch's
// awaitReady and FindRunning both check, standing in for an already-running
// mlx_audio.server without needing the real binary.
func listenRoot(t *testing.T) (runtime audio.ServerTrusted, closeFn func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	addr := srv.Listener.Addr().(*net.TCPAddr)
	return audio.ServerTrusted{Host: addr.IP.String(), Port: addr.Port}, srv.Close
}

func TestAcquireVoiceBackend(t *testing.T) {
	t.Run("no speaker in the composition is an error", func(t *testing.T) {
		plan, backend, err := acquireVoiceBackend(t.Context(), &evoke.Composition{}, audio.Trusted{}, audio.ServerTrusted{}, 0, nil, nil, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "no voice to speak with")
		require.Nil(t, plan)
		require.Nil(t, backend)
	})

	t.Run("an already-running server is connected to, not launched", func(t *testing.T) {
		runtime, closeFn := listenRoot(t)
		defer closeFn()

		var statuses []string
		report := func(s string) { statuses = append(statuses, s) }

		plan, backend, err := acquireVoiceBackend(t.Context(), voiceComposition(), audio.Trusted{}, runtime, 0, nil, nil, report)

		require.NoError(t, err)
		require.NotNil(t, plan)
		require.NotNil(t, backend)
		require.Equal(t, "http://"+runtime.Addr(), backend.Endpoint())
		require.Len(t, statuses, 1)
		require.Contains(t, statuses[0], "Using the voice backend already running")
	})

	t.Run("an existing plan and backend are reused without doing I/O", func(t *testing.T) {
		// No listener on this address at all: if acquireVoiceBackend tried to
		// reach it, the report callback (nil here) would have to run to explain
		// what happened — proving it never gets that far is the point of passing
		// a live backend in already.
		runtime := audio.ServerTrusted{Host: "127.0.0.1", Port: 1}
		existingPlan := &audio.Plan{Model: "already-compiled"}
		existingBackend := audio.Connect(runtime)

		plan, backend, err := acquireVoiceBackend(t.Context(), &evoke.Composition{}, audio.Trusted{}, runtime, 0, existingPlan, existingBackend, func(string) {
			t.Fatal("report must not be called when the backend is already set")
		})

		require.NoError(t, err)
		require.Same(t, existingPlan, plan)
		require.Same(t, existingBackend, backend)
	})
}

func TestChatVoiceToggle(t *testing.T) {
	t.Run("turning on acquires a backend and persists the setting", func(t *testing.T) {
		runtime, closeFn := listenRoot(t)
		defer closeFn()
		c := newChatVoice(voiceComposition(), audio.Trusted{}, runtime, "", 0)
		sess := chat.NewSession(testChatPlan())

		err := c.toggle(t.Context(), sess, nil)

		require.NoError(t, err)
		require.True(t, c.on())
		require.True(t, sess.VoiceOn())
		require.NotNil(t, c.backend)
	})

	t.Run("turning off needs no backend and persists the setting", func(t *testing.T) {
		c := chatVoice{enabled: true, backend: audio.Connect(audio.ServerTrusted{})}
		sess := chat.NewSession(testChatPlan())

		err := c.toggle(t.Context(), sess, func(string) {
			t.Fatal("turning off must do no I/O")
		})

		require.NoError(t, err)
		require.False(t, c.on())
		require.False(t, sess.VoiceOn())
	})

	t.Run("a failed acquisition leaves voice off", func(t *testing.T) {
		c := newChatVoice(&evoke.Composition{}, audio.Trusted{}, audio.ServerTrusted{}, "", 0)
		sess := chat.NewSession(testChatPlan())

		err := c.toggle(t.Context(), sess, nil)

		require.Error(t, err)
		require.False(t, c.on())
		require.False(t, sess.VoiceOn())
	})

	t.Run("re-enabling after an off cycle reuses the backend already acquired", func(t *testing.T) {
		runtime, closeFn := listenRoot(t)
		defer closeFn()
		c := newChatVoice(voiceComposition(), audio.Trusted{}, runtime, "", 0)
		sess := chat.NewSession(testChatPlan())
		require.NoError(t, c.toggle(t.Context(), sess, nil))
		firstBackend := c.backend

		require.NoError(t, c.toggle(t.Context(), sess, nil)) // off
		require.False(t, c.on())

		require.NoError(t, c.toggle(t.Context(), sess, func(string) {
			t.Fatal("re-enabling must not redo the acquisition")
		})) // on again

		require.True(t, c.on())
		require.Same(t, firstBackend, c.backend)
	})
}

func TestChatVoiceSpeak(t *testing.T) {
	t.Run("off is a no-op", func(t *testing.T) {
		c := chatVoice{enabled: false}
		require.NoError(t, c.speak(t.Context(), "hello"))
	})

	t.Run("an empty reply is a no-op", func(t *testing.T) {
		c := chatVoice{enabled: true, plan: &audio.Plan{}, backend: audio.Connect(audio.ServerTrusted{})}
		require.NoError(t, c.speak(t.Context(), "   "))
	})

	t.Run("on speaks through the backend and plays the result", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("fake-audio"))
		}))
		defer srv.Close()
		addr := srv.Listener.Addr().(*net.TCPAddr)

		c := chatVoice{
			enabled: true,
			plan:    &audio.Plan{Model: "test-model", Format: "wav"},
			backend: audio.Connect(audio.ServerTrusted{Host: addr.IP.String(), Port: addr.Port}),
			player:  fakePlayerScript(t),
		}

		require.NoError(t, c.speak(t.Context(), "Sit down."))
	})
}

// fakePlayerScript writes a script standing in for afplay that always
// succeeds, so speak's playback step runs without a real audio device.
func fakePlayerScript(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-player.sh")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700))
	return path
}

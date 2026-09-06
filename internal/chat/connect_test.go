package chat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

// llamaProps fakes llama-server's /props, in the shape the real server serves.
func llamaProps(modelPath string, nCtx int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/props" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// The real payload nests n_ctx under default_generation_settings and has
		// no top-level context size; model_path is top-level.
		_, _ = w.Write([]byte(`{"model_path":"` + modelPath +
			`","default_generation_settings":{"n_ctx":` + strconv.Itoa(nCtx) + `},"total_slots":4}`))
	}
}

func mlxModels(ids ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		body := `{"object":"list","data":[`
		for i, id := range ids {
			if i > 0 {
				body += ","
			}
			body += `{"id":"` + id + `","object":"model"}`
		}
		_, _ = w.Write([]byte(body + `]}`))
	}
}

// ownedModels serves a /v1/models list whose entries carry an owned_by value,
// as llama-server's do.
func ownedModels(owner string, ids ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		body := `{"object":"list","data":[`
		for i, id := range ids {
			if i > 0 {
				body += ","
			}
			body += `{"id":"` + id + `","object":"model","owned_by":"` + owner + `"}`
		}
		_, _ = w.Write([]byte(body + `]}`))
	}
}

// planAt builds a plan pointing at a test server's host and port.
func planAt(t *testing.T, server *httptest.Server, backend, modelPath string, contextWindow int) *Plan {
	t.Helper()
	u, err := url.Parse(server.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(u.Port())
	require.NoError(t, err)
	return &Plan{
		Backend:   backend,
		ModelPath: modelPath,
		Runtime:   RuntimeSpec{Host: u.Hostname(), Port: port, ContextWindow: contextWindow},
	}
}

func TestFindRunning(t *testing.T) {
	tests := []struct {
		name          string
		handler       http.HandlerFunc
		backend       string
		modelPath     string
		contextWindow int
		// wantNotOurs marks a case where what is listening is not this backend
		// at all, which is an error rather than a fit to weigh.
		wantNotOurs    bool
		wantModel      string
		wantContext    int
		wantMismatches []string
		// contextOnly marks a case where the model matches and only the context
		// falls short, so the consequence describes truncation, not the model.
		contextOnly bool
	}{
		{
			name:          "llama.cpp serving the same model is compatible",
			handler:       llamaProps("/models/nemo.gguf", 8192),
			backend:       backendLlamaCpp,
			modelPath:     "/models/nemo.gguf",
			contextWindow: 8192,
			wantModel:     "/models/nemo.gguf",
			wantContext:   8192,
		},
		{
			name:          "llama.cpp serving another model does not match",
			handler:       llamaProps("/models/other.gguf", 8192),
			backend:       backendLlamaCpp,
			modelPath:     "/models/nemo.gguf",
			contextWindow: 8192,
			wantModel:     "/models/other.gguf",
			wantContext:   8192,
			// llama-server ignores the model named in a request, so this is the
			// case that would silently answer from the wrong model.
			wantMismatches: []string{"it has /models/other.gguf loaded"},
		},
		{
			name:          "llama.cpp with too small a context does not match",
			handler:       llamaProps("/models/nemo.gguf", 4096),
			backend:       backendLlamaCpp,
			modelPath:     "/models/nemo.gguf",
			contextWindow: 32768,
			wantModel:     "/models/nemo.gguf",
			wantContext:   4096,
			wantMismatches: []string{
				"its context is 4096 tokens, short of the 32768 this chat budgets for",
			},
			contextOnly: true,
		},
		{
			name:          "mlx launched from a local path reports it last",
			handler:       mlxModels("mlx-community/Qwen3-8B-4bit", "mlx-community/other-4bit", "/models/qwen"),
			backend:       backendMLX,
			modelPath:     "/models/qwen",
			contextWindow: 131072,
			wantModel:     "/models/qwen",
			// MLX reports no context because it has none, so the budget check
			// never fires even at 131072.
		},
		{
			name:          "mlx launched from a repo id reports no model",
			handler:       mlxModels("mlx-community/Qwen3-8B-4bit", "mlx-community/other-4bit"),
			backend:       backendMLX,
			modelPath:     "mlx-community/Qwen3-8B-4bit",
			contextWindow: 8192,
			// A cached repo id says the model is downloaded, never that it is
			// loaded, so the list is not evidence of a match.
			wantMismatches: []string{"it does not report which model it loaded"},
		},
		{
			name:           "mlx serving another model does not match",
			handler:        mlxModels("/models/gemma"),
			backend:        backendMLX,
			modelPath:      "/models/qwen",
			contextWindow:  8192,
			wantModel:      "/models/gemma",
			wantMismatches: []string{"it has /models/gemma loaded"},
		},
		{
			// llama-server serves /v1/models too, with an absolute path as the
			// id, so the shape alone is not enough to identify a runtime.
			name:          "a llama.cpp server is not mistaken for mlx",
			handler:       ownedModels("llamacpp", "/models/nemo.gguf"),
			backend:       backendMLX,
			modelPath:     "/models/nemo.gguf",
			contextWindow: 8192,
			wantNotOurs:   true,
		},
		{
			name:          "something that is not a backend at all is an error",
			handler:       func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) },
			backend:       backendMLX,
			modelPath:     "/models/qwen",
			contextWindow: 8192,
			wantNotOurs:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()
			plan := planAt(t, server, tt.backend, tt.modelPath, tt.contextWindow)

			running, err := FindRunning(t.Context(), plan)

			if tt.wantNotOurs {
				require.ErrorContains(t, err, "is not a "+tt.backend+" backend")
				require.Nil(t, running)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, running)
			want := &Running{
				Endpoint:      running.Endpoint,
				Model:         tt.wantModel,
				ContextWindow: tt.wantContext,
				Mismatches:    tt.wantMismatches,
			}
			if len(tt.wantMismatches) > 0 {
				drv, _ := driverFor(tt.backend)
				want.Consequence = drv.mismatchConsequence
				if tt.contextOnly {
					want.Consequence = shortContextConsequence
				}
				require.NotEmpty(t, want.Consequence, "a mismatch must explain what connecting anyway does")
			}
			require.Equal(t, want, running)
		})
	}
}

func TestFindRunningFreePort(t *testing.T) {
	// A closed server leaves its port free, which is the signal to launch.
	server := httptest.NewServer(http.NotFoundHandler())
	plan := planAt(t, server, backendMLX, "/models/qwen", 8192)
	server.Close()

	running, err := FindRunning(t.Context(), plan)

	require.NoError(t, err)
	require.Nil(t, running, "nothing listening is the signal to launch")
}

func TestConnectOwnsNothing(t *testing.T) {
	b := Connect(&Plan{Runtime: RuntimeSpec{Host: "127.0.0.1", Port: 8080}})

	require.Equal(t, "http://127.0.0.1:8080/v1", b.Endpoint().String())
	require.False(t, b.Launched(), "Evoke did not start this server")
	require.NoError(t, b.Close(context.Background()), "closing must not stop a process Evoke did not start")
	require.NoError(t, b.Err())
	require.Empty(t, b.DrainLog(), "its output belongs to whoever started it")
	// A nil Done channel blocks forever in a select, which is the correct
	// semantic: there is no child process whose exit could be awaited.
	require.Nil(t, b.Done())
}

func TestSameModel(t *testing.T) {
	// A runtime reports the path it resolved; the plan holds the path it was
	// given, and on macOS a temp dir is itself a symlink.
	dir := t.TempDir()
	link := t.TempDir() + "/link"
	require.NoError(t, os.Symlink(dir, link))

	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{name: "identical references", a: "/models/nemo.gguf", b: "/models/nemo.gguf", want: true},
		{name: "different references", a: "/models/nemo.gguf", b: "/models/other.gguf", want: false},
		{name: "repo ids compare literally", a: "mlx-community/Qwen3-8B-4bit", b: "mlx-community/Qwen3-8B-4bit", want: true},
		{name: "a symlink resolves to its target", a: link, b: dir, want: true},
		{name: "missing paths that differ are not equal", a: "/nope/a", b: "/nope/b", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, sameModel(tt.a, tt.b))
		})
	}
}

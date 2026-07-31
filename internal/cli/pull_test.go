package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/jesse0michael/evoke/internal/client"
	"github.com/stretchr/testify/require"
)

func TestPull(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		handler  http.HandlerFunc
		wantCode int
		wantFile string
	}{
		{
			name:     "no args prints usage",
			args:     []string{},
			wantCode: 2,
		},
		{
			name:     "invalid ref without @",
			args:     []string{"ns/name"},
			wantCode: 1,
		},
		{
			name:     "invalid ref missing name",
			args:     []string{"@ns"},
			wantCode: 1,
		},
		{
			name: "successful pull",
			args: []string{"@test-ns/test-name"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/v1/test-ns/test-name":
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(client.VersionList{
						Versions: []client.Version{{Version: 1, Sha256: "abc123"}},
					})
				case r.Method == http.MethodGet && r.URL.Path == "/v1/test-ns/test-name/1":
					w.Header().Set("Content-Type", "text/plain")
					_, _ = w.Write([]byte("NAME\n    Test Character\n"))
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			},
			wantCode: 0,
			wantFile: "test-ns/test-name.evoke",
		},
		{
			name: "registry error",
			args: []string{"@test-ns/missing"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantCode: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("EVOKE_HOME", dir)

			if tt.handler != nil {
				srv := httptest.NewServer(tt.handler)
				defer srv.Close()

				s := &Settings{Registry: srv.URL}
				require.NoError(t, saveSettings(s))
			}

			code := Pull(tt.args, false)
			require.Equal(t, tt.wantCode, code)

			if tt.wantFile != "" {
				libDir := filepath.Join(dir, "library")
				data, err := os.ReadFile(filepath.Join(libDir, tt.wantFile))
				require.NoError(t, err)
				require.Contains(t, string(data), "NAME")

				// Check manifest was updated.
				m, err := manifest()
				require.NoError(t, err)
				require.Contains(t, m.Artifacts, "@test-ns/test-name")
			}
		})
	}
}

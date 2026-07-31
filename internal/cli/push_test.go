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

func TestPush(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		file     string
		handler  http.HandlerFunc
		wantCode int
	}{
		{
			name:     "missing args prints usage",
			args:     []string{},
			wantCode: 2,
		},
		{
			name:     "too many args prints usage",
			args:     []string{"a.evoke", "ns/name", "extra"},
			wantCode: 2,
		},
		{
			name:     "invalid target format",
			args:     []string{"a.evoke", "nonamespace"},
			file:     "NAME test\n",
			wantCode: 2,
		},
		{
			name:     "file does not exist",
			args:     []string{"nonexistent.evoke", "ns/name"},
			wantCode: 1,
		},
		{
			name:     "invalid evoke syntax",
			args:     []string{"bad.evoke", "ns/name"},
			file:     "UNKNOWN_DECL\n    value\n",
			wantCode: 1,
		},
		{
			name: "successful push",
			args: []string{"good.evoke", "test-ns/test-name"},
			file: "NAME\n    Test Character\n",
			handler: func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodPut, r.Method)
				require.Equal(t, "/v1/test-ns/test-name", r.URL.Path)
				require.Equal(t, "Bearer test-access-token", r.Header.Get("Authorization"))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(client.Version{
					Version: 1,
					Sha256:  "abcdef123456abcdef123456abcdef123456abcdef123456abcdef123456abcd",
				})
			},
			wantCode: 0,
		},
		{
			name: "unauthorized response",
			args: []string{"good.evoke", "ns/name"},
			file: "NAME\n    Test Character\n",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(client.ErrorResponse{
					Errors: []client.Error{{Message: "token expired"}},
				})
			},
			wantCode: 1,
		},
		{
			name: "bad request response",
			args: []string{"good.evoke", "ns/name"},
			file: "NAME\n    Test Character\n",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(client.ErrorResponse{
					Errors: []client.Error{{Message: "duplicate content"}},
				})
			},
			wantCode: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("EVOKE_HOME", dir)

			// Write the .evoke file if provided.
			if tt.file != "" {
				for _, name := range []string{"good.evoke", "bad.evoke", "a.evoke"} {
					p := filepath.Join(dir, name)
					_ = os.WriteFile(p, []byte(tt.file), 0o644)
				}
			}

			// Set up a fake registry if a handler is provided.
			if tt.handler != nil {
				srv := httptest.NewServer(tt.handler)
				defer srv.Close()

				creds := &Credentials{
					Registry:    srv.URL,
					Username:    "test-user",
					AccessToken: "test-access-token",
				}
				require.NoError(t, saveCredentials(creds))
			}

			// Rewrite file paths in args to use the temp directory.
			args := make([]string, len(tt.args))
			for i, a := range tt.args {
				if filepath.Ext(a) == ".evoke" {
					args[i] = filepath.Join(dir, a)
				} else {
					args[i] = a
				}
			}

			code := Push(args, false)
			require.Equal(t, tt.wantCode, code)
		})
	}
}

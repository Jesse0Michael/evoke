package ollama

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// tagsServer serves ollama's /api/tags with the given model names.
func tagsServer(t *testing.T, names []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/tags", r.URL.Path)
		models := ""
		for i, n := range names {
			if i > 0 {
				models += ","
			}
			models += fmt.Sprintf(`{"name":%q}`, n)
		}
		fmt.Fprintf(w, `{"models":[%s]}`, models)
	}))
}

// A server that is already answering is used as-is and never owned, so Close
// must leave it running.
func TestEnsureAdoptsRunningServer(t *testing.T) {
	tests := []struct {
		name      string
		installed []string
		want      []string
		wantErr   string
	}{
		{
			name:      "no models required",
			installed: nil,
			want:      nil,
		},
		{
			name:      "exact name matches",
			installed: []string{"nomic-embed-text"},
			want:      []string{"nomic-embed-text"},
		},
		{
			// ollama reports an untagged pull as name:latest.
			name:      "implicit latest tag matches",
			installed: []string{"nomic-embed-text:latest"},
			want:      []string{"nomic-embed-text"},
		},
		{
			name:      "missing model names the pull command",
			installed: []string{"llama3:latest"},
			want:      []string{"nomic-embed-text"},
			wantErr:   "ollama pull nomic-embed-text",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := tagsServer(t, tt.installed)
			defer srv.Close()

			got, err := Ensure(t.Context(), srv.URL, tt.want, 0)

			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.False(t, got.Started())
			// Closing an adopted server is a no-op: it is still answering.
			require.NoError(t, got.Close(t.Context()))
			require.True(t, answering(t.Context(), &http.Client{}, srv.URL))
		})
	}
}

// Launching is only ever a local act; a configured remote endpoint that is down
// is an error rather than a reason to start a different server here.
func TestEnsureDoesNotLaunchForRemoteEndpoint(t *testing.T) {
	_, err := Ensure(t.Context(), "http://not-a-real-host.invalid:11434", nil, 0)

	require.ErrorContains(t, err, "is not reachable")
}

func TestAddress(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		want     string
		loopback bool
		wantErr  bool
	}{
		{name: "explicit port", url: "http://127.0.0.1:11434", want: "127.0.0.1:11434", loopback: true},
		{name: "localhost name", url: "http://localhost:11434", want: "localhost:11434", loopback: true},
		{name: "default http port", url: "http://embed.internal", want: "embed.internal:80"},
		{name: "default https port", url: "https://embed.internal", want: "embed.internal:443"},
		{name: "ipv6 loopback", url: "http://[::1]:11434", want: "[::1]:11434", loopback: true},
		{name: "no host", url: "not a url", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := address(tt.url)

			require.Equal(t, tt.wantErr, err != nil)
			require.Equal(t, tt.want, got)
			if !tt.wantErr {
				require.Equal(t, tt.loopback, isLoopback(got))
			}
		})
	}
}

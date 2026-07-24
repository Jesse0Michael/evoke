package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jesse0michael/evoke/internal/client"
	"github.com/stretchr/testify/require"
)

// TestExchangeIDToken drives the generated registry client against a stub
// server, covering the ID-token-to-credentials mapping used by login.
func TestExchangeIDToken(t *testing.T) {
	tests := []struct {
		name     string
		handler  http.HandlerFunc
		wantErr  bool
		wantUser string
	}{
		{
			name: "success maps token response to credentials",
			handler: func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/v1/tokens/google", r.URL.Path)
				var body client.GoogleLoginRequest
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				require.Equal(t, "google-id-token", body.IdToken)
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(client.TokenResponse{
					Subject:      "test-user",
					AccessToken:  "our-access",
					RefreshToken: "our-refresh",
				})
			},
			wantUser: "test-user",
		},
		{
			name: "non-200 is an error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"errors":[{"message":"invalid id token"}]}`))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			creds, err := exchangeIDToken(t.Context(), srv.URL, "google-id-token")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantUser, creds.Username)
			require.Equal(t, srv.URL, creds.Registry)
			require.Equal(t, "our-access", creds.AccessToken)
		})
	}
}

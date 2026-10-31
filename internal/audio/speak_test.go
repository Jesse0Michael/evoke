package audio

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSpeak(t *testing.T) {
	tests := []struct {
		name            string
		plan            *Plan
		text            string
		wantFields      map[string]any
		respond         func(w http.ResponseWriter)
		expectBody      []byte
		wantUnsupported []string
		wantErr         string
	}{
		{
			name: "a described speaker travels as instruct",
			plan: &Plan{Model: "test-model", Instruct: "low alto, unhurried", Format: "wav"},
			text: "Sit down.",
			wantFields: map[string]any{
				"model": "test-model", "input": "Sit down.",
				"instruct": "low alto, unhurried", "response_format": "wav",
			},
			respond:    func(w http.ResponseWriter) { _, _ = w.Write([]byte("fake-audio-bytes")) },
			expectBody: []byte("fake-audio-bytes"),
		},
		{
			name: "a voice preset travels as voice",
			plan: &Plan{Model: "test-model", Settings: map[string]string{"voice": "alloy"}, Format: "wav"},
			text: "hi",
			wantFields: map[string]any{
				"model": "test-model", "input": "hi", "voice": "alloy", "response_format": "wav",
			},
		},
		{
			name: "a reference clip travels with its transcript",
			plan: &Plan{Model: "test-model", Reference: "/clips/sumi.wav", ReferenceText: "Sit down.", Format: "wav"},
			text: "hi",
			wantFields: map[string]any{
				"model": "test-model", "input": "hi", "response_format": "wav",
				"ref_audio": "/clips/sumi.wav", "ref_text": "Sit down.",
			},
		},
		{
			// The same AUDIO block must mean the same thing through either
			// transport, so every setting the one-shot runtime takes as a flag
			// is sent here as a field — numerically, since the server types them.
			name: "sampling settings reach the server as typed fields",
			plan: &Plan{Model: "test-model", Format: "wav", Settings: map[string]string{
				"speed": "1.2", "temperature": "0.7", "top_k": "40", "lang_code": "a", "max_tokens": "900",
			}},
			text: "hi",
			wantFields: map[string]any{
				"model": "test-model", "input": "hi", "response_format": "wav",
				"speed": 1.2, "temperature": 0.7, "top_k": float64(40),
				"lang_code": "a", "max_tokens": float64(900),
			},
		},
		{
			// cfg_scale and ddpm_steps have no field in the server's schema, so
			// they are reported rather than dropped without a word.
			name: "settings the server has no field for are reported",
			plan: &Plan{Model: "test-model", Format: "wav", Settings: map[string]string{
				"cfg_scale": "3.0", "ddpm_steps": "30", "speed": "1.1",
			}},
			text: "hi",
			wantFields: map[string]any{
				"model": "test-model", "input": "hi", "response_format": "wav", "speed": 1.1,
			},
			wantUnsupported: []string{"cfg_scale", "ddpm_steps"},
		},
		{
			name: "server error",
			plan: &Plan{Model: "test-model", Format: "wav"},
			text: "hi",
			respond: func(w http.ResponseWriter) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte("model not found"))
			},
			wantErr: "voice backend returned 400",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
				if tt.respond != nil {
					tt.respond(w)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()
			backend := &Backend{endpoint: srv.URL}

			data, unsupported, err := Speak(t.Context(), backend, tt.plan, tt.text)

			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantUnsupported, unsupported)
			if tt.wantFields != nil {
				require.Equal(t, tt.wantFields, got)
			}
			if tt.expectBody != nil {
				require.Equal(t, tt.expectBody, data)
			}
		})
	}
}

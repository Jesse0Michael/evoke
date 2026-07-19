// Package oidc verifies OpenID Connect ID tokens and returns normalized
// identity claims. It is deliberately provider-agnostic: everything except the
// NewGoogle constructor is written against generic OIDC, so this package is a
// candidate for extraction into github.com/jesse0michael/pkg/auth/oidc once it
// has settled. The registry only ever consumes the Verifier interface.
package oidc

import (
	"context"
	"fmt"
	"slices"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
)

// GoogleIssuer is Google's OIDC issuer URL.
const GoogleIssuer = "https://accounts.google.com"

// Claims are the normalized identity claims extracted from a verified ID token.
type Claims struct {
	// Provider is the login method these claims came from, e.g. "google".
	Provider string
	// Subject is the provider's stable, unique identifier for the user (sub).
	Subject string
	Email   string
	// EmailVerified reports whether the provider vouches for the email.
	EmailVerified bool
	Name          string
	Picture       string
}

// Verifier validates a raw ID token and returns its normalized claims. A verify
// failure (bad signature, wrong audience, expired) is returned as an error;
// callers map it to 401.
type Verifier interface {
	Verify(ctx context.Context, rawIDToken string) (*Claims, error)
}

// googleVerifier verifies Google-issued ID tokens against Google's JWKS.
type googleVerifier struct {
	verifier  *gooidc.IDTokenVerifier
	clientIDs []string
}

// NewGoogle builds a Verifier for Google ID tokens. clientIDs are the OAuth
// clients whose tokens are accepted — a token is valid if its audience matches
// any of them (e.g. the Desktop client for the CLI and the Web client for the
// site). Audience is checked here rather than by go-oidc so multiple clients
// can be accepted. It performs OIDC discovery against Google, so it needs
// network at construction time.
func NewGoogle(ctx context.Context, clientIDs []string) (Verifier, error) {
	provider, err := gooidc.NewProvider(ctx, GoogleIssuer)
	if err != nil {
		return nil, fmt.Errorf("failed to discover google oidc provider: %w", err)
	}
	return &googleVerifier{
		verifier:  provider.Verifier(&gooidc.Config{SkipClientIDCheck: true}),
		clientIDs: clientIDs,
	}, nil
}

func (g *googleVerifier) Verify(ctx context.Context, rawIDToken string) (*Claims, error) {
	tok, err := g.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("failed to verify id token: %w", err)
	}

	// Fail closed: with no configured clients, no audience is acceptable.
	if !audienceAllowed(tok.Audience, g.clientIDs) {
		return nil, fmt.Errorf("token audience %v is not an accepted client", tok.Audience)
	}

	var raw struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		Picture       string `json:"picture"`
	}
	if err := tok.Claims(&raw); err != nil {
		return nil, fmt.Errorf("failed to parse id token claims: %w", err)
	}

	return &Claims{
		Provider:      "google",
		Subject:       tok.Subject,
		Email:         raw.Email,
		EmailVerified: raw.EmailVerified,
		Name:          raw.Name,
		Picture:       raw.Picture,
	}, nil
}

// audienceAllowed reports whether any of the token's audiences is an accepted
// client. Empty allowed set → nothing is accepted.
func audienceAllowed(tokenAud, allowed []string) bool {
	for _, a := range tokenAud {
		if slices.Contains(allowed, a) {
			return true
		}
	}
	return false
}

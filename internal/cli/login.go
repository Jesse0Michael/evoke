// Package cli implements the evoke commands. cmd/cli owns dispatch and usage;
// each command's implementation lives here so it is testable as a normal
// package (mirroring how cmd/app delegates to internal/*).
package cli

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/jesse0michael/evoke/internal/client"
	"github.com/kelseyhightower/envconfig"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// loginConfig is read from the environment. GOOGLE_CLIENT_ID/SECRET are the
// Desktop OAuth client the CLI authenticates with; the registry accepts tokens
// audienced to that client.
type loginConfig struct {
	RegistryURL  string `envconfig:"EVOKE_REGISTRY_URL" default:"http://localhost:8080"`
	ClientID     string `envconfig:"GOOGLE_CLIENT_ID"`
	ClientSecret string `envconfig:"GOOGLE_CLIENT_SECRET"`
}

// loginTimeout bounds how long we wait for the user to finish in the browser.
const loginTimeout = 3 * time.Minute

// Login runs the Google loopback + PKCE sign-in and stores the registry tokens.
func Login(args []string) int {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	registryFlag := fs.String("registry", "", "registry base URL (overrides $EVOKE_REGISTRY_URL)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	var cfg loginConfig
	if err := envconfig.Process("", &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "evoke login: %v\n", err)
		return 1
	}
	if *registryFlag != "" {
		cfg.RegistryURL = *registryFlag
	}
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		fmt.Fprintln(os.Stderr, "evoke login: GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET must be set (the Desktop OAuth client)")
		return 2
	}

	creds, err := runLogin(context.Background(), cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke login: %v\n", err)
		return 1
	}
	if err := saveCredentials(creds); err != nil {
		fmt.Fprintf(os.Stderr, "evoke login: %v\n", err)
		return 1
	}

	fmt.Printf("Logged in as %s (%s)\n", creds.Username, cfg.RegistryURL)
	return 0
}

// runLogin performs the browser loopback + PKCE flow: it obtains a Google ID
// token, exchanges it at the registry, and returns the registry's tokens.
func runLogin(ctx context.Context, cfg loginConfig) (*Credentials, error) {
	// A throwaway listener on a random loopback port receives Google's redirect.
	// Desktop OAuth clients allow any 127.0.0.1 port, so nothing is pre-registered.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to start local callback server: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURL := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	oauthCfg := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint:     google.Endpoint,
		RedirectURL:  redirectURL,
		Scopes:       []string{"openid", "email", "profile"},
	}

	verifier := oauth2.GenerateVerifier()
	state, err := randomState()
	if err != nil {
		return nil, err
	}
	authURL := oauthCfg.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.S256ChallengeOption(verifier))

	code, err := captureCode(ctx, listener, state, authURL)
	if err != nil {
		return nil, err
	}

	// Exchange the code with Google for tokens (client secret + PKCE verifier).
	googleTok, err := oauthCfg.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, fmt.Errorf("failed to exchange authorization code: %w", err)
	}
	idToken, ok := googleTok.Extra("id_token").(string)
	if !ok || idToken == "" {
		return nil, errors.New("google response did not include an id_token")
	}

	// Exchange the Google ID token for the registry's own tokens.
	return exchangeIDToken(ctx, cfg.RegistryURL, idToken)
}

// exchangeIDToken posts the Google ID token to the registry using the generated
// client and returns the registry's own tokens.
func exchangeIDToken(ctx context.Context, registryURL, idToken string) (*Credentials, error) {
	c, err := client.NewClientWithResponses(registryURL)
	if err != nil {
		return nil, fmt.Errorf("failed to build registry client: %w", err)
	}
	resp, err := c.LoginWithGoogleWithResponse(ctx, client.GoogleLoginRequest{IdToken: idToken})
	if err != nil {
		return nil, fmt.Errorf("failed to reach registry at %s: %w", registryURL, err)
	}
	if resp.JSON200 == nil {
		return nil, fmt.Errorf("registry rejected login (%s): %s", resp.HTTPResponse.Status, strings.TrimSpace(string(resp.Body)))
	}
	tok := resp.JSON200
	return &Credentials{
		Registry:     registryURL,
		Username:     tok.User.Username,
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
	}, nil
}

// captureCode opens the browser to authURL, serves the loopback listener until
// Google redirects back, and returns the authorization code.
func captureCode(ctx context.Context, listener net.Listener, state, authURL string) (string, error) {
	type result struct {
		code string
		err  error
	}
	resCh := make(chan result, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if e := q.Get("error"); e != "" {
			writePage(w, "Login failed — you can close this window.")
			resCh <- result{err: fmt.Errorf("authorization denied: %s", e)}
			return
		}
		if q.Get("state") != state {
			writePage(w, "Login failed (state mismatch) — you can close this window.")
			resCh <- result{err: errors.New("state mismatch — possible CSRF, aborting")}
			return
		}
		writePage(w, "Login successful — you can close this window and return to the terminal.")
		resCh <- result{code: q.Get("code")}
	})

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(listener) }()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	fmt.Println("Opening your browser to sign in with Google…")
	fmt.Printf("\nIf it doesn't open automatically, visit:\n\n  %s\n\n", authURL)
	if err := openBrowser(authURL); err != nil {
		// Non-fatal: the URL was printed above.
		fmt.Fprintf(os.Stderr, "(couldn't open a browser automatically: %v)\n", err)
	}

	select {
	case res := <-resCh:
		return res.code, res.err
	case <-time.After(loginTimeout):
		return "", errors.New("timed out waiting for the browser sign-in")
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// randomState returns a URL-safe random state parameter.
func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// writePage sends a minimal HTML page to the browser tab.
func writePage(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, "<!doctype html><html><body style=\"font-family:sans-serif;text-align:center;padding-top:4rem\"><h2>%s</h2></body></html>", msg)
}

// openBrowser opens url in the default browser for the current OS.
func openBrowser(url string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "windows":
		cmd, args = "rundll32", []string{"url.dll,FileProtocolHandler"}
	default:
		cmd = "xdg-open"
	}
	return exec.Command(cmd, append(args, url)...).Start()
}

package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"time"
)

// probeTimeout bounds each identification request. The endpoint is already
// known to be listening, so a slow answer means something that is not the
// server Evoke expects.
const probeTimeout = 2 * time.Second

// shortContextConsequence explains connecting to a server whose context is
// smaller than the plan's. It is runtime-independent: the sliding window is
// Evoke's, and it would keep counting turns the server has already dropped.
const shortContextConsequence = "The conversation would be budgeted for a window the backend does not have, " +
	"so it would discard history that Evoke still counts as present."

// Running describes a server already listening on the endpoint a plan targets,
// and how well it fits that plan.
type Running struct {
	// Endpoint is the host:port it was found on.
	Endpoint string
	// Model is what the server reports having loaded, empty when the runtime
	// cannot report it.
	Model string
	// ContextWindow is the context it was started with, zero when the runtime
	// has no context limit to report.
	ContextWindow int
	// Mismatches lists the reasons this server does not fit the plan. Empty
	// means it does and the session can simply use it; otherwise the caller
	// decides, since the cost is the user's to weigh.
	Mismatches []string
	// Consequence explains what using it despite the mismatch would do. It
	// names the problem actually found — the wrong model and too small a
	// context are different failures — and is empty when there is no mismatch.
	Consequence string
}

// FindRunning reports the server already listening on the endpoint the plan
// targets. It returns nil when nothing is listening, which is the signal to
// Launch one. It returns an error when something is listening that is not this
// backend at all, since that is not a fit to weigh but a port to clear.
//
// Identification is per runtime and best-effort: it answers "what did this
// process load", never "is this process trustworthy". The endpoint is loopback
// and the alternative is refusing to run at all.
func FindRunning(ctx context.Context, p *Plan) (*Running, error) {
	drv, ok := driverFor(p.Backend)
	if !ok {
		return nil, fmt.Errorf("unsupported chat backend %q", p.Backend)
	}
	addr := endpointAddr(p)
	if !inUse(addr) {
		return nil, nil
	}
	notOurs := fmt.Errorf("refusing to start backend: %s is already in use by something that is not a %s backend; stop it or set a different chat.port in settings",
		addr, p.Backend)
	if drv.probe == nil {
		return nil, notOurs
	}

	client := &http.Client{Timeout: probeTimeout}
	identity, err := drv.probe(ctx, client, "http://"+addr)
	if err != nil {
		return nil, notOurs
	}

	r := &Running{Endpoint: addr, Model: identity.Model, ContextWindow: identity.ContextWindow}
	wrongModel := true
	switch {
	case r.Model == "":
		r.Mismatches = append(r.Mismatches, "it does not report which model it loaded")
	case !sameModel(r.Model, p.ModelPath):
		r.Mismatches = append(r.Mismatches, fmt.Sprintf("it has %s loaded", r.Model))
	default:
		wrongModel = false
	}
	// A reported context smaller than the plan's budget means Evoke would be
	// promising a window the server cannot hold, and the server would silently
	// truncate behind the sliding window's back.
	if r.ContextWindow > 0 && r.ContextWindow < p.Runtime.ContextWindow {
		r.Mismatches = append(r.Mismatches, fmt.Sprintf("its context is %d tokens, short of the %d this chat budgets for",
			r.ContextWindow, p.Runtime.ContextWindow))
	}
	switch {
	case wrongModel:
		r.Consequence = drv.mismatchConsequence
	case len(r.Mismatches) > 0:
		r.Consequence = shortContextConsequence
	}
	return r, nil
}

// Connect returns a Backend for a server that was already running. Evoke owns
// no process, so closing it stops nothing and there is no child whose exit
// could be watched.
func Connect(p *Plan) *Backend {
	return &Backend{endpoint: baseURL(endpointAddr(p))}
}

// identity is what a probe can learn about a running server. A zero field means
// the runtime does not expose it, not that it is unset — mlx reports no context
// because it has none, and no model when it was launched from a repo id.
type identity struct {
	Model         string
	ContextWindow int
}

// sameModel reports whether two references name the same model. Paths are
// compared with symlinks resolved, since a runtime reports the path it resolved
// while the plan holds the path it was given.
func sameModel(a, b string) bool {
	if a == b {
		return true
	}
	ra, aerr := filepath.EvalSymlinks(a)
	rb, berr := filepath.EvalSymlinks(b)
	return aerr == nil && berr == nil && ra == rb
}

// getJSON performs a GET and decodes a JSON body, failing on any non-2xx.
func getJSON(ctx context.Context, client *http.Client, endpoint string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s returned %s", endpoint, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

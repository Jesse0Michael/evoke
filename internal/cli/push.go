package cli

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/jesse0michael/evoke/internal/client"
	evoke "github.com/jesse0michael/evoke/pkg/evoke"
)

// Push reads a local .evoke file, validates it, and pushes it to the registry
// as a new immutable version under the given namespace/name.
func Push(args []string, verbose bool) int {
	fs := flag.NewFlagSet("push", flag.ContinueOnError)
	fs.BoolVar(&verbose, "v", verbose, "verbose output")
	fs.BoolVar(&verbose, "verbose", verbose, "verbose output")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	posArgs := fs.Args()
	if len(posArgs) != 2 {
		fmt.Fprintln(os.Stderr, "usage: evoke push <file> <namespace/name>")
		return 2
	}

	filePath := posArgs[0]
	target := posArgs[1]

	namespace, name, ok := strings.Cut(target, "/")
	if !ok || namespace == "" || name == "" {
		fmt.Fprintf(os.Stderr, "evoke push: target must be namespace/name, got %q\n", target)
		return 2
	}

	// Read and validate the file.
	content, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke push: %v\n", err)
		return 1
	}

	doc, err := evoke.Parse(content)
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke push: failed to parse %s: %v\n", filePath, err)
		return 1
	}

	if err := evoke.Validate(doc); err != nil {
		fmt.Fprintf(os.Stderr, "evoke push: %v\n", err)
		return 1
	}

	// Load credentials.
	creds, err := loadCredentials()
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintln(os.Stderr, "evoke push: not logged in — run `evoke login` first")
		} else {
			fmt.Fprintf(os.Stderr, "evoke push: %v\n", err)
		}
		return 1
	}

	registryURL := creds.Registry
	if registryURL == "" {
		registryURL = "http://localhost:8080"
	}

	ctx := context.Background()

	c, err := client.NewClientWithResponses(registryURL, client.WithRequestEditorFn(
		func(_ context.Context, req *http.Request) error {
			req.Header.Set("Authorization", "Bearer "+creds.AccessToken)
			return nil
		},
	))
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke push: failed to create registry client: %v\n", err)
		return 1
	}

	resp, err := c.PushVersionWithTextBodyWithResponse(ctx, namespace, name, string(content))
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke push: %v\n", err)
		return 1
	}

	switch {
	case resp.JSON201 != nil:
		v := resp.JSON201
		fmt.Printf("Pushed %s/%s v%d (sha256:%s)\n", namespace, name, v.Version, v.Sha256[:12])
		return 0
	case resp.JSON401 != nil:
		fmt.Fprintln(os.Stderr, "evoke push: unauthorized — try `evoke login` to refresh credentials")
		return 1
	case resp.JSON400 != nil:
		msgs := make([]string, len(resp.JSON400.Errors))
		for i, e := range resp.JSON400.Errors {
			msgs[i] = e.Message
		}
		fmt.Fprintf(os.Stderr, "evoke push: bad request: %s\n", strings.Join(msgs, "; "))
		return 1
	default:
		fmt.Fprintf(os.Stderr, "evoke push: unexpected response %s\n", resp.Status())
		return 1
	}
}

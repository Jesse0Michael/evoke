package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Pull downloads one or more registry references (@namespace/name) to the local
// library, updating the manifest. This is the explicit form of the implicit pull
// that happens during image/chat/inspect when a registry ref is encountered.
func Pull(args []string, verbose bool) int {
	fs := flag.NewFlagSet("pull", flag.ContinueOnError)
	fs.BoolVar(&verbose, "v", verbose, "verbose output")
	fs.BoolVar(&verbose, "verbose", verbose, "verbose output")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	refs := fs.Args()
	if len(refs) == 0 {
		fmt.Fprintln(os.Stderr, "usage: evoke pull @namespace/name [...]")
		return 2
	}

	s, err := settings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke pull: %v\n", err)
		return 1
	}

	m, err := manifest()
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke pull: %v\n", err)
		return 1
	}

	registryURL := s.Registry
	if registryURL == "" {
		registryURL = "http://localhost:8080"
	}

	libDir, err := library()
	if err != nil {
		fmt.Fprintf(os.Stderr, "evoke pull: %v\n", err)
		return 1
	}

	ctx := context.Background()
	var failed int

	for _, ref := range refs {
		if !strings.HasPrefix(ref, "@") {
			fmt.Fprintf(os.Stderr, "evoke pull: invalid reference %q (expected @namespace/name)\n", ref)
			failed++
			continue
		}
		namespace, name := parseRegistryRef(ref)
		if namespace == "" || name == "" {
			fmt.Fprintf(os.Stderr, "evoke pull: invalid reference %q (expected @namespace/name)\n", ref)
			failed++
			continue
		}

		libPath := filepath.Join(libDir, namespace, name+".evoke")

		_, sha, err := pullFromRegistry(ctx, registryURL, namespace, name, libPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "evoke pull: %v\n", err)
			failed++
			continue
		}

		// Update manifest.
		key := "@" + namespace + "/" + name
		relPath := "library/" + namespace + "/" + name + ".evoke"
		m.Artifacts[key] = Artifact{
			File:     relPath,
			Registry: registryURL,
			Revision: "latest",
			SHA256:   sha,
		}

		fmt.Printf("Pulled %s/%s (sha256:%s)\n", namespace, name, sha[:12])
	}

	if err := saveManifest(m); err != nil {
		fmt.Fprintf(os.Stderr, "evoke pull: %v\n", err)
		return 1
	}

	if failed > 0 {
		return 1
	}
	return 0
}

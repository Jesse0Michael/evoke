package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInspectComposition(t *testing.T) {
	// Every target resolved to one file: the files are listed, then their merged
	// composition is rendered.
	dir := t.TempDir()
	files := map[string]string{
		"name.evoke":   "NAME\n    Aria\n",
		"outfit.evoke": "APPAREL\n    red dress\n",
	}
	for name, src := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644))
	}

	results := []inspectResult{
		{input: classifiedInput{Raw: "name"}, candidates: []indexCandidate{{Path: filepath.Join(dir, "name.evoke")}}},
		{input: classifiedInput{Raw: "outfit"}, candidates: []indexCandidate{{Path: filepath.Join(dir, "outfit.evoke")}}},
	}

	var out bytes.Buffer
	require.Equal(t, 0, inspectComposition(&out, results, []sourceRoot{{Path: dir}}, chatStyle{}))

	expected := "name => name.evoke\n" +
		"outfit => outfit.evoke\n" +
		"\n" +
		"NAME\n    Aria\n\nAPPAREL\n    red dress\n"
	require.Equal(t, expected, out.String())
}

func TestInspectGroups(t *testing.T) {
	// One target matches several files, one matches nothing: each is grouped
	// under its raw input, and the empty target is called out.
	results := []inspectResult{
		{input: classifiedInput{Raw: "wardrobe"}, candidates: []indexCandidate{
			{Path: "/dir/beta.evoke"},
			{Path: "/dir/alpha.evoke"},
		}},
		{input: classifiedInput{Raw: "missing"}, candidates: nil},
	}

	var out bytes.Buffer
	inspectGroups(&out, results, chatStyle{})

	expected := "wardrobe:\n" +
		"alpha.evoke\n" +
		"beta.evoke\n" +
		"\n" +
		"missing:\n" +
		"  (no matches)\n"
	require.Equal(t, expected, out.String())
}

func TestInspectList(t *testing.T) {
	// Candidates deliberately out of order to exercise the filename sort.
	// evoke inspect's listing is just the matching filenames — never a
	// file's tags, declared or local.
	candidates := []indexCandidate{
		{Path: "/dir/gamma.evoke", Name: "gamma"},
		{Path: "/dir/beta.evoke", Name: "beta"},
		{Path: "/dir/alpha.evoke", Name: "alpha"},
	}

	var out bytes.Buffer
	inspectList(&out, candidates, chatStyle{})

	expected := "alpha.evoke\n" +
		"beta.evoke\n" +
		"gamma.evoke\n"
	require.Equal(t, expected, out.String())
}

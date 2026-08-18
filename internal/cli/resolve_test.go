package cli

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func testPick(input, path string) variantPick {
	return variantPick{input: input, candidate: indexCandidate{Path: path}}
}

func TestCrossProduct(t *testing.T) {
	tests := []struct {
		name  string
		slots [][]variantPick
		want  [][]variantPick
	}{
		{
			name:  "no slots yields one empty combination",
			slots: nil,
			want:  [][]variantPick{{}},
		},
		{
			name:  "single slot yields one combination per match",
			slots: [][]variantPick{{testPick("character", "a.evoke"), testPick("character", "b.evoke")}},
			want: [][]variantPick{
				{testPick("character", "a.evoke")},
				{testPick("character", "b.evoke")},
			},
		},
		{
			name: "leftmost slot varies slowest",
			slots: [][]variantPick{
				{testPick("character", "a.evoke"), testPick("character", "b.evoke")},
				{testPick("pose", "x.evoke"), testPick("pose", "y.evoke"), testPick("pose", "z.evoke")},
			},
			want: [][]variantPick{
				{testPick("character", "a.evoke"), testPick("pose", "x.evoke")},
				{testPick("character", "a.evoke"), testPick("pose", "y.evoke")},
				{testPick("character", "a.evoke"), testPick("pose", "z.evoke")},
				{testPick("character", "b.evoke"), testPick("pose", "x.evoke")},
				{testPick("character", "b.evoke"), testPick("pose", "y.evoke")},
				{testPick("character", "b.evoke"), testPick("pose", "z.evoke")},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, crossProduct(tt.slots))
		})
	}
}

func TestDescribeSlots(t *testing.T) {
	tests := []struct {
		name  string
		slots [][]variantPick
		want  string
	}{
		{
			name:  "no slots",
			slots: nil,
			want:  "",
		},
		{
			name:  "single slot",
			slots: [][]variantPick{{testPick("character", "a.evoke"), testPick("character", "b.evoke")}},
			want:  "character 2",
		},
		{
			name: "multiple slots",
			slots: [][]variantPick{
				{testPick("character", "a.evoke"), testPick("character", "b.evoke")},
				{testPick("pose", "x.evoke")},
			},
			want: "character 2 x pose 1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, describeSlots(tt.slots))
		})
	}
}

// testResolution indexes dir and returns a resolution over the given selectors.
func testResolution(t *testing.T, dir string, selectors ...string) *resolution {
	t.Helper()
	idx := newTestIndex(t)
	root := sourceRoot{Path: dir, Kind: sourceKindCurrent}
	require.NoError(t, idx.ensureRoot(t.Context(), root))

	inputs := make([]classifiedInput, 0, len(selectors))
	for _, s := range selectors {
		inputs = append(inputs, classifiedInput{Raw: s, Kind: inputSelector})
	}
	return &resolution{selectorInputs: inputs, idx: idx, roots: []sourceRoot{root}}
}

func TestResolutionVariants(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EVOKE_HOME", t.TempDir())

	for _, name := range []string{"bob", "alice"} {
		createEvokeFile(t, dir, name+".evoke", "TAGS\n    character\n\nNAME\n    "+name+"\n")
	}
	for _, name := range []string{"kneel", "stand"} {
		createEvokeFile(t, dir, name+".evoke", "TAGS\n    pose\n\nAPPEARANCE\n    "+name+"ing\n")
	}

	res := testResolution(t, dir, "character", "pose")

	variants, err := res.variants(t.Context())
	require.NoError(t, err)

	got := make([][]string, 0, len(variants))
	for _, v := range variants {
		names := make([]string, 0, len(v))
		for _, p := range v {
			names = append(names, filepath.Base(p.candidate.Path))
		}
		got = append(got, names)
	}

	// Sorted by path within each slot, leftmost varying slowest.
	require.Equal(t, [][]string{
		{"alice.evoke", "kneel.evoke"},
		{"alice.evoke", "stand.evoke"},
		{"bob.evoke", "kneel.evoke"},
		{"bob.evoke", "stand.evoke"},
	}, got)

	// Every combination loads into a document set ordered to match the picks.
	docs, err := res.documentsFor(variants[2])
	require.NoError(t, err)
	require.Len(t, docs, 2)
	require.Equal(t, filepath.Join(dir, "bob.evoke"), docs[0].Source)
	require.Equal(t, filepath.Join(dir, "kneel.evoke"), docs[1].Source)
}

func TestResolutionVariantsSingleSelector(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EVOKE_HOME", t.TempDir())

	createEvokeFile(t, dir, "alice.evoke", "TAGS\n    character\n\nNAME\n    Alice\n")

	res := testResolution(t, dir, "character")

	variants, err := res.variants(t.Context())
	require.NoError(t, err)
	require.Len(t, variants, 1)
	require.Equal(t, filepath.Join(dir, "alice.evoke"), variants[0][0].candidate.Path)
}

func TestResolutionVariantsExceedsLimit(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EVOKE_HOME", t.TempDir())

	for i := range maxBatch + 1 {
		name := fmt.Sprintf("test-character-%03d.evoke", i)
		createEvokeFile(t, dir, name, "TAGS\n    character\n\nAPPEARANCE\n    dark hair\n")
	}

	res := testResolution(t, dir, "character")

	_, err := res.variants(t.Context())
	require.Error(t, err)
	require.Equal(t,
		"xall would generate 101 combinations (character 101), more than the limit of 100; narrow the selection or drop xall",
		err.Error())
}

func TestResolutionVariantsNoMatch(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EVOKE_HOME", t.TempDir())

	createEvokeFile(t, dir, "alice.evoke", "TAGS\n    character\n\nNAME\n    Alice\n")

	res := testResolution(t, dir, "nonexistent")

	_, err := res.variants(t.Context())
	require.Error(t, err)
	require.Equal(t, `no files match selector "nonexistent"`, err.Error())
}

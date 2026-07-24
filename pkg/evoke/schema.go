package evoke

type MergeMode string

const (
	MergeSingular     MergeMode = "singular"
	MergeAccumulating MergeMode = "accumulating"
)

type Definition struct {
	Name             string
	Merge            MergeMode
	Negative         bool
	Default          bool
	AcceptsArgument  bool
	RequiresArgument bool
	Structured       bool
	MixedContent     bool
	Order            int
}

var builtins = []Definition{
	{Name: "NAME", Merge: MergeSingular, Order: 10},
	{Name: "CHARACTER", Merge: MergeAccumulating, Order: 20},
	{Name: "PERSONALITY", Merge: MergeAccumulating, Negative: true, Default: true, Order: 30},
	{Name: "BACKSTORY", Merge: MergeAccumulating, Order: 40},
	{Name: "APPEARANCE", Merge: MergeAccumulating, Negative: true, Default: true, Order: 50},
	{Name: "APPAREL", Merge: MergeAccumulating, Negative: true, Default: true, Order: 60},
	{Name: "ENVIRONMENT", Merge: MergeAccumulating, Negative: true, Default: true, Order: 70},
	{Name: "SCENARIO", Merge: MergeSingular, Default: true, Order: 80},
	{Name: "PROMPT", Merge: MergeAccumulating, Negative: true, Default: true, Order: 90},
	{Name: "IMAGE", Merge: MergeSingular, Negative: true, Default: true, AcceptsArgument: true, Structured: true, MixedContent: true, Order: 100},
	{Name: "LORA", Merge: MergeSingular, Default: true, AcceptsArgument: true, RequiresArgument: true, Structured: true, Order: 110},
	{Name: "DETAILER", Merge: MergeSingular, Negative: true, Default: true, AcceptsArgument: true, RequiresArgument: true, Structured: true, MixedContent: true, Order: 120},
}

var byName = func() map[string]Definition {
	m := make(map[string]Definition, len(builtins))
	for _, d := range builtins {
		m[d.Name] = d
	}
	return m
}()

var migrationAliases = map[string]string{
	"IDENTITY": "CHARACTER",
}

func LookupDeclaration(name string) (Definition, bool) {
	d, ok := byName[name]
	return d, ok
}

func MigrationAlias(name string) string {
	return migrationAliases[name]
}

func AllDeclarations() []Definition {
	out := make([]Definition, len(builtins))
	copy(out, builtins)
	return out
}

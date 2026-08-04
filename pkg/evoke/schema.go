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
	// VOICE is what the subject sounds like — the audible counterpart of
	// APPEARANCE, which is why it renders between the language blocks and the
	// drawable ones. No target consumes it yet: it is tracked, merged, and
	// inspectable so characters can carry a voice before an audio target exists.
	// When one does, engine and model configuration belongs in a structured
	// declaration of its own (APPEARANCE is to IMAGE what VOICE is to that),
	// not in settings bolted onto this block.
	//
	// Negative is deliberately on and deliberately undocumented. No voice API
	// takes a negative description — the negative prompt is a classifier-free
	// guidance artifact, and description-conditioned speech models read the
	// description as language instead, so !VOICE would most likely render as an
	// "avoid" clause the way chat relabels !PERSONALITY, not as a subtraction.
	// The channel stays because dropping it later would invalidate files while
	// adding it later would not, and the docs and style guide stay silent so
	// nobody writes !VOICE expecting !APPEARANCE semantics. Keep it that way:
	// the prefix tables state the capability, nothing prescribes its use.
	{Name: "VOICE", Merge: MergeAccumulating, Negative: true, Default: true, Order: 45},
	{Name: "APPEARANCE", Merge: MergeAccumulating, Negative: true, Default: true, Order: 50},
	{Name: "APPAREL", Merge: MergeAccumulating, Negative: true, Default: true, Order: 60},
	{Name: "ENVIRONMENT", Merge: MergeAccumulating, Negative: true, Default: true, Order: 70},
	{Name: "SCENARIO", Merge: MergeSingular, Default: true, Order: 80},
	{Name: "PROMPT", Merge: MergeAccumulating, Negative: true, Default: true, Order: 90},
	{Name: "IMAGE", Merge: MergeSingular, Negative: true, Default: true, AcceptsArgument: true, Structured: true, MixedContent: true, Order: 100},
	{Name: "LORA", Merge: MergeSingular, Default: true, AcceptsArgument: true, RequiresArgument: true, Structured: true, Order: 110},
	{Name: "DETAILER", Merge: MergeSingular, Negative: true, Default: true, AcceptsArgument: true, RequiresArgument: true, Structured: true, MixedContent: true, Order: 120},
	{Name: "CHAT", Merge: MergeSingular, Default: true, Structured: true, MixedContent: true, Order: 130},
	{Name: "KNOWLEDGE", Merge: MergeSingular, Default: true, AcceptsArgument: true, RequiresArgument: true, Structured: true, Order: 140},
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

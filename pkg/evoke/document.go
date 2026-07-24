package evoke

// Declaration is one parsed declaration block from a .evoke file.
type Declaration struct {
	Name     string
	RawName  string
	Argument string
	Negative bool
	Default  bool
	Values   []string
	Line     int
}

// Metadata holds document-level information parsed from source.
type Metadata struct {
	Tags []string
}

// Document is a parsed .evoke source file.
type Document struct {
	Source       string
	Metadata     Metadata
	Declarations []*Declaration
}

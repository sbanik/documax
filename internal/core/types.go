package core

type docFormat string

type diagnostic struct {
	line    int
	message string
}
type docFile struct {
	path     string
	line     int
	content  []byte
	encoding string
}
type docDirectory struct {
	path  string
	line  int
	files []docFile
}
type document struct {
	format      docFormat
	directories []docDirectory
}
type tag struct {
	kind, path, encoding string
	start, end, line     int
}

type packItem struct{ rel, full string }

type unpackFile struct {
	path string
	data []byte
}
type unpackPlan struct {
	dirs  []string
	files []unpackFile
}

// TreeEntry describes a directory or file selected by Documax packing rules.
type TreeEntry struct {
	IsDir bool
}

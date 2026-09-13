package core

import "regexp"

const DefaultDocFile = "documax-output.txt"

const (
	formatBracket docFormat = "bracket"
	formatXML     docFormat = "xml"
)

// defaultIgnoredPaths lists operating-system metadata, IDE state, language
// caches, and generated build artifacts that Documax never packs. Each name
// applies at any depth in the source tree. Add project-specific exclusions to
// .documax.ignore rather than widening this shared list.
var defaultIgnoredPaths = []string{
	".git",
	".DS_Store",
	".DS_STORE",
	"Thumbs.db",
	"ehthumbs.db",
	"desktop.ini",
	"Icon\\r",
	"._*",
	".Spotlight-V100",
	".Trashes",
	".fseventsd",
	".idea",
	".vscode",
	".vs",
	"__pycache__",
	"__pylance__",
	".pytest_cache",
	".mypy_cache",
	".ruff_cache",
	".tox",
	".nox",
	".venv",
	"venv",
	"node_modules",
	".gradle",
	"target",
	"build",
	"dist",
	"bin",
	"obj",
	"documax",
	"documax.exe",
	"*.test",
	"*.out",
	"coverage.out",
	"*.swp",
	"*.swo",
	"*~",
}

var bracketTagRE = regexp.MustCompile(`\[DIR: ([^\]\r\n]+)\]|\[FILE: ([^\]\r\n]+)\]|\[/FILE\]|\[/DIR\]`)
var xmlTagRE = regexp.MustCompile(`<d:dir\s+path="([^"]+)"\s*>|<d:file\s+path="([^"]+)"(?:\s+encoding="gzip\+base64")?\s*>|</d:file>|</d:dir>`)

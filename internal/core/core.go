// Package core implements Documax document parsing and file operations.
package core

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"

	gitignore "github.com/go-git/go-git/v5/plumbing/format/gitignore"
	"github.com/schollz/progressbar/v3"
)

func parseDocument(raw []byte) (document, []diagnostic) {
	doc := document{format: detectFormat(raw)}
	var ds []diagnostic
	tags := parseTags(raw, doc.format)
	if len(tags) == 0 {
		return doc, []diagnostic{{line: 1, message: "no Documax structural tags found"}}
	}
	var currentDir *docDirectory
	var currentFile *docFile
	contentStart := 0
	for _, t := range tags {
		if indented(raw, t.start) {
			ds = append(ds, diagnostic{t.line, "syntax tag must not be indented"})
		}
		switch t.kind {
		case "dir-start":
			if currentFile != nil {
				ds = append(ds, diagnostic{t.line, "missing FILE closing tag before DIRECTORY start tag"})
				currentFile = nil
			}
			if currentDir != nil {
				ds = append(ds, diagnostic{t.line, "nested DIRECTORY section is not allowed"})
			}
			doc.directories = append(doc.directories, docDirectory{path: t.path, line: t.line})
			currentDir = &doc.directories[len(doc.directories)-1]
		case "dir-end":
			if currentDir == nil {
				ds = append(ds, diagnostic{t.line, "unexpected DIRECTORY closing tag"})
				continue
			}
			if currentFile != nil {
				ds = append(ds, diagnostic{t.line, "missing FILE closing tag before DIRECTORY closing tag"})
				currentFile = nil
			}
			currentDir = nil
		case "file-start":
			if currentDir == nil {
				ds = append(ds, diagnostic{t.line, "FILE tag appears outside a DIRECTORY section"})
				continue
			}
			if currentFile != nil {
				ds = append(ds, diagnostic{t.line, "missing FILE closing tag before next FILE tag"})
			}
			f := docFile{path: t.path, line: t.line, encoding: t.encoding}
			currentDir.files = append(currentDir.files, f)
			currentFile = &currentDir.files[len(currentDir.files)-1]
			contentStart = t.end
		case "file-end":
			if currentFile == nil {
				ds = append(ds, diagnostic{t.line, "unexpected FILE closing tag"})
				continue
			}
			body := append([]byte(nil), raw[contentStart:t.start]...)
			if currentFile.encoding == "" {
				body = stripOneLeadingNewline(body)
			}
			currentFile.content = body
			if currentFile.encoding != "" && currentFile.encoding != "GZ+B64" {
				ds = append(ds, diagnostic{currentFile.line, fmt.Sprintf("unsupported encoding %q", currentFile.encoding)})
			}
			if currentFile.encoding == "GZ+B64" {
				if _, err := gzipBase64Decode(body); err != nil {
					ds = append(ds, diagnostic{currentFile.line, "invalid GZ+B64 file content"})
				}
			}
			currentFile = nil
		}
	}
	if currentFile != nil {
		ds = append(ds, diagnostic{currentFile.line, "unclosed FILE section at EOF"})
	}
	if currentDir != nil {
		ds = append(ds, diagnostic{currentDir.line, "unclosed DIRECTORY section at EOF"})
	}
	return doc, ds
}

func detectFormat(raw []byte) docFormat {
	if bytes.Contains(raw, []byte("<d:dir")) || bytes.Contains(raw, []byte("<d:file")) {
		return formatXML
	}
	return formatBracket
}

func parseTags(raw []byte, format docFormat) []tag {
	re := bracketTagRE
	if format == formatXML {
		re = xmlTagRE
	}
	var tags []tag
	for _, m := range re.FindAllSubmatchIndex(raw, -1) {
		text := string(raw[m[0]:m[1]])
		t := tag{start: m[0], end: m[1], line: lineAt(raw, m[0])}
		switch {
		case m[2] >= 0:
			t.kind, t.path = "dir-start", string(raw[m[2]:m[3]])
		case m[4] >= 0:
			t.kind, t.path = "file-start", string(raw[m[4]:m[5]])
			if (format == formatBracket && strings.Contains(t.path, ";ENC=GZ+B64")) || (format == formatXML && strings.Contains(text, `encoding="gzip+base64"`)) {
				t.encoding = "GZ+B64"
				t.path = strings.TrimSuffix(t.path, ";ENC=GZ+B64")
			}
		case text == "[/FILE]" || text == "</d:file>":
			t.kind = "file-end"
		default:
			t.kind = "dir-end"
		}
		tags = append(tags, t)
	}
	return tags
}

func indented(raw []byte, start int) bool {
	lineStart := bytes.LastIndexByte(raw[:start], '\n') + 1
	return start > lineStart && len(bytes.Trim(raw[lineStart:start], " \t")) == 0
}

func stripOneLeadingNewline(b []byte) []byte {
	if bytes.HasPrefix(b, []byte("\r\n")) {
		return b[2:]
	}
	if bytes.HasPrefix(b, []byte("\n")) {
		return b[1:]
	}
	return b
}
func lineAt(b []byte, i int) int { return bytes.Count(b[:i], []byte("\n")) + 1 }
func printDiagnostics(file string, ds []diagnostic) {
	for _, d := range ds {
		fmt.Printf("%s:%d: %s\n", file, d.line, d.message)
	}
}
func validateDocumax(file string) (bool, []string) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return false, []string{err.Error()}
	}
	_, ds := parseDocument(raw)
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = fmt.Sprintf("%s:%d: %s", file, d.line, d.message)
	}
	return len(out) == 0, out
}

// ValidateFile checks a Documax document and returns compiler-style diagnostics.
func ValidateFile(file string) (bool, []string) { return validateDocumax(file) }

// FixFile applies conservative syntax repairs to a document.
func FixFile(file string, inPlace bool) error { return runFix(file, inPlace) }

// MinimizeFile compresses each file payload as GZ+B64.
func MinimizeFile(file, output string) error { return runMinify(file, output) }

// ExpandFile decodes GZ+B64 file payloads into readable source.
func ExpandFile(file, output string) error { return runExpand(file, output) }

// NewSignalContext returns a context cancelled on Ctrl+C or SIGTERM.
func NewSignalContext() (context.Context, context.CancelFunc) { return setupSignalContext() }

// PackDirectory writes an expanded Documax document from a directory.
func PackDirectory(ctx context.Context, source, output, format string) error {
	return runPackDir(ctx, source, output, docFormat(format))
}

// PackMinimized writes a GZ+B64 minimized Documax document from a directory.
func PackMinimized(ctx context.Context, source, output, format string, interactive bool) error {
	return runPackMinimized(ctx, source, output, docFormat(format), interactive)
}

// PackInteractive creates a document from pasted file paths and content.
func PackInteractive(ctx context.Context, scope, output, format string) error {
	return runPackInteractive(ctx, scope, output, docFormat(format))
}

// Unpack extracts an expanded or minimized Documax document.
func Unpack(ctx context.Context, file, root, subpath string, allowAbsolute, fixInPlace bool) error {
	return runUnpack(ctx, file, root, subpath, allowAbsolute, fixInPlace)
}

func fixDocument(raw []byte) ([]byte, []diagnostic, bool) {
	format := detectFormat(raw)
	indentRE := regexp.MustCompile(`(?m)^[\t ]+(?:\[(?:DIR:|FILE:|/FILE|/DIR)|<d:)`)
	fixed := indentRE.ReplaceAllFunc(raw, func(match []byte) []byte {
		return bytes.TrimLeft(match, " \t")
	})
	changed := !bytes.Equal(raw, fixed)
	var notes []diagnostic
	if changed {
		notes = append(notes, diagnostic{1, "removed indentation from syntax tags"})
	}
	tags := parseTags(fixed, format)
	inDir, inFile := false, false
	inserts := map[int]string{}
	addEnd := func(pos, line int) {
		if _, ok := inserts[pos]; ok {
			return
		}
		s := fileEnd(format)
		if pos == len(fixed) {
			if pos > 0 && fixed[pos-1] != '\n' {
				s = "\n" + s
			}
			s += "\n"
		} else if pos == 0 || fixed[pos-1] == '\n' {
			s += "\n"
		}
		inserts[pos] = s
		notes = append(notes, diagnostic{line, "inserted missing FILE closing tag"})
		changed = true
	}
	for _, t := range tags {
		switch t.kind {
		case "dir-start":
			if inFile {
				addEnd(t.start, t.line)
				inFile = false
			}
			inDir = true
		case "dir-end":
			if inFile {
				addEnd(t.start, t.line)
				inFile = false
			}
			inDir = false
		case "file-start":
			if inDir {
				inFile = true
			}
		case "file-end":
			inFile = false
		}
	}
	if inFile {
		addEnd(len(fixed), lineAt(fixed, len(fixed)))
	}
	if inDir {
		prefix := ""
		if len(fixed) > 0 && fixed[len(fixed)-1] != '\n' {
			prefix = "\n"
		}
		inserts[len(fixed)] += prefix + dirEnd(format) + "\n"
		notes = append(notes, diagnostic{lineAt(fixed, len(fixed)), "inserted missing DIRECTORY closing tag"})
		changed = true
	}
	if len(inserts) == 0 {
		return fixed, notes, changed
	}
	var ps []int
	for p := range inserts {
		ps = append(ps, p)
	}
	sort.Ints(ps)
	var out bytes.Buffer
	last := 0
	for _, p := range ps {
		out.Write(fixed[last:p])
		out.WriteString(inserts[p])
		last = p
	}
	out.Write(fixed[last:])
	return out.Bytes(), notes, changed
}

func runFix(file string, inPlace bool) error {
	raw, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	fixed, notes, changed := fixDocument(raw)
	_, remaining := parseDocument(fixed)
	if len(remaining) > 0 {
		printDiagnostics(file, remaining)
		return errors.New("document contains errors that cannot be fixed safely")
	}
	if changed {
		printDiagnostics(file, notes)
	}
	if inPlace {
		return os.WriteFile(file, fixed, 0644)
	}
	_, err = os.Stdout.Write(fixed)
	return err
}

func decodedContent(f docFile) ([]byte, error) {
	data := f.content
	if f.encoding == "GZ+B64" {
		return gzipBase64Decode(data)
	}
	return data, nil
}
func gzipBase64Encode(data []byte) (string, error) {
	var compressed bytes.Buffer
	w := gzip.NewWriter(&compressed)
	if _, err := w.Write(data); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(compressed.Bytes()), nil
}
func gzipBase64Decode(data []byte) ([]byte, error) {
	compressed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, err
	}
	r, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	return io.ReadAll(r)
}
func directoryHeader(format docFormat, p string) string {
	if format == formatXML {
		return `<d:dir path="` + p + `">`
	}
	return "[DIR: " + p + "]"
}
func fileHeader(format docFormat, p string, encoded bool) string {
	if format == formatXML {
		if encoded {
			return `<d:file path="` + p + `" encoding="gzip+base64">`
		}
		return `<d:file path="` + p + `">`
	}
	if encoded {
		return "[FILE: " + p + ";ENC=GZ+B64]"
	}
	return "[FILE: " + p + "]"
}
func fileEnd(format docFormat) string {
	if format == formatXML {
		return "</d:file>"
	}
	return "[/FILE]"
}

// payloadNeedsEncoding reports whether raw file bytes would be interpreted as
// Documax structure if emitted verbatim in this document format.
func payloadNeedsEncoding(data []byte, format docFormat) bool {
	return len(parseTags(data, format)) > 0
}
func dirEnd(format docFormat) string {
	if format == formatXML {
		return "</d:dir>"
	}
	return "[/DIR]"
}

func runMinify(file, output string) error {
	raw, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	doc, ds := parseDocument(raw)
	if len(ds) > 0 {
		printDiagnostics(file, ds)
		return errors.New("cannot minimize an invalid document")
	}
	var out bytes.Buffer
	for _, d := range doc.directories {
		out.WriteString(directoryHeader(doc.format, d.path))
		for _, f := range d.files {
			data, err := decodedContent(f)
			if err != nil {
				return err
			}
			encoded, err := gzipBase64Encode(data)
			if err != nil {
				return err
			}
			out.WriteString(fileHeader(doc.format, f.path, true))
			out.WriteString(encoded)
			out.WriteString(fileEnd(doc.format))
		}
		out.WriteString(dirEnd(doc.format))
	}
	return writeOutput(output, out.Bytes())
}
func runExpand(file, output string) error {
	raw, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	doc, ds := parseDocument(raw)
	if len(ds) > 0 {
		printDiagnostics(file, ds)
		return errors.New("cannot expand an invalid document")
	}
	return writeOutput(output, renderExpanded(doc))
}
func renderExpanded(doc document) []byte {
	var out bytes.Buffer
	for _, d := range doc.directories {
		out.WriteString(directoryHeader(doc.format, d.path))
		out.WriteByte('\n')
		for _, f := range d.files {
			data, err := decodedContent(f)
			if err != nil {
				continue
			}
			body := string(data)
			encoded := payloadNeedsEncoding(data, doc.format)
			if encoded {
				body, err = gzipBase64Encode(data)
				if err != nil {
					continue
				}
			}
			out.WriteString(fileHeader(doc.format, f.path, encoded))
			out.WriteByte('\n')
			out.WriteString(body)
			if !strings.HasSuffix(body, "\n") {
				out.WriteByte('\n')
			}
			out.WriteString(fileEnd(doc.format))
			out.WriteByte('\n')
		}
		out.WriteString(dirEnd(doc.format))
		out.WriteByte('\n')
	}
	return out.Bytes()
}
func writeOutput(path string, data []byte) error {
	if path == "" {
		_, err := os.Stdout.Write(data)
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func runPackMinimized(ctx context.Context, source, output string, format docFormat, interactive bool) error {
	outputPath, err := filepath.Abs(output)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(outputPath), ".documax-expanded-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Remove(tempPath); err != nil {
		return err
	}
	defer func() { _ = os.Remove(tempPath) }()

	if interactive {
		err = runPackInteractive(ctx, source, tempPath, format)
	} else {
		if source == "" {
			return errors.New("missing --dir flag for directory packing")
		}
		err = runPackDir(ctx, source, tempPath, format)
	}
	if err != nil {
		return err
	}
	return runMinify(tempPath, output)
}

func runPackDir(ctx context.Context, source, output string, formats ...docFormat) error {
	format := formatBracket
	if len(formats) > 0 {
		format = formats[0]
	}
	if format != formatBracket && format != formatXML {
		return errors.New("format must be bracket or xml")
	}
	abs, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	ignorer := buildGitIgnore(abs)
	var files []packItem
	var dirs []string
	err = filepath.Walk(abs, func(p string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		rel, _ := filepath.Rel(abs, p)
		rel = filepath.ToSlash(rel)
		if isDefaultIgnored(rel) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			dirs = append(dirs, rel)
			if rel != "." && ignorer.Match(strings.Split(rel, "/"), true) {
				return filepath.SkipDir
			}
			return nil
		}
		if rel == ".documax.ignore" {
			return nil
		}
		if rel != ".gitignore" && ignorer.Match(strings.Split(rel, "/"), false) {
			return nil
		}
		files = append(files, packItem{rel, p})
		return nil
	})
	if err != nil {
		return err
	}
	var emptyDirs []string
	for _, dir := range dirs {
		if dir == "." {
			continue
		}
		hasFile := false
		for _, file := range files {
			if strings.HasPrefix(file.rel, dir+"/") {
				hasFile = true
				break
			}
		}
		if !hasFile {
			emptyDirs = append(emptyDirs, dir)
		}
	}
	fmt.Printf("Phase 1 complete: directories %d/%d, files %d/%d\n", len(dirs), len(dirs), len(files), len(files))
	outAbs, err := filepath.Abs(output)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(outAbs), ".documax-pack-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = tmp.Close(); _ = os.Remove(tmpName) }()
	w := bufio.NewWriter(tmp)
	root := filepath.Base(abs) + "/"
	if _, err = w.WriteString(directoryHeader(format, root) + "\n"); err != nil {
		return err
	}
	bar := progressbar.Default(int64(len(files)), "Packing files")
	for _, item := range files {
		select {
		case <-ctx.Done():
			return errors.New("packing interrupted; no output was published")
		default:
		}
		data, err := os.ReadFile(item.full)
		if err != nil {
			return err
		}
		body := string(data)
		encoded := payloadNeedsEncoding(data, format)
		if encoded {
			body, err = gzipBase64Encode(data)
			if err != nil {
				return err
			}
		}
		if _, err = w.WriteString(fileHeader(format, item.rel, encoded)); err != nil {
			return err
		}
		if err = w.WriteByte('\n'); err != nil {
			return err
		}
		if _, err = w.WriteString(body); err != nil {
			return err
		}
		if !strings.HasSuffix(body, "\n") {
			if _, err = w.WriteString("\n"); err != nil {
				return err
			}
		}
		if _, err = w.WriteString(fileEnd(format) + "\n"); err != nil {
			return err
		}
		_ = bar.Add(1)
	}
	if _, err = w.WriteString(dirEnd(format) + "\n"); err != nil {
		return err
	}
	for _, dir := range emptyDirs {
		if _, err = w.WriteString(directoryHeader(format, root+dir+"/") + "\n" + dirEnd(format) + "\n"); err != nil {
			return err
		}
	}
	if err = w.Flush(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, outAbs)
}

func isDefaultIgnored(rel string) bool {
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		for _, ignored := range defaultIgnoredPaths {
			if part == ignored {
				return true
			}
			if strings.ContainsAny(ignored, "*?[") {
				matched, err := pathpkg.Match(ignored, part)
				if err == nil && matched {
					return true
				}
			}
		}
	}
	return false
}

// CollectPackableTree returns the relative directory and file tree that would
// be included by PackDirectory. It is used by developer validation tooling.
func CollectPackableTree(root string) (map[string]TreeEntry, error) {
	ignorer := buildGitIgnore(root)
	entries := map[string]TreeEntry{}
	err := filepath.Walk(root, func(current string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if isDefaultIgnored(rel) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			if rel != "." && ignorer.Match(strings.Split(rel, "/"), true) {
				return filepath.SkipDir
			}
			entries[rel] = TreeEntry{IsDir: true}
			return nil
		}
		if rel == ".documax.ignore" || (rel != ".gitignore" && ignorer.Match(strings.Split(rel, "/"), false)) {
			return nil
		}
		entries[rel] = TreeEntry{}
		return nil
	})
	return entries, err
}

func runPackInteractive(ctx context.Context, scope, output string, formats ...docFormat) error {
	format := formatBracket
	if len(formats) > 0 {
		format = formats[0]
	}
	if format != formatBracket && format != formatXML {
		return errors.New("format must be bracket or xml")
	}
	r := bufio.NewReader(os.Stdin)
	if scope == "" {
		fmt.Print("Enter base directory path: ")
		line, _ := r.ReadString('\n')
		scope = strings.TrimSpace(line)
		if scope == "" {
			return errors.New("empty directory path: exiting")
		}
	}
	doc := document{format: format, directories: []docDirectory{{path: filepath.ToSlash(scope)}}}
	for {
		select {
		case <-ctx.Done():
			return errors.New("packing interrupted")
		default:
		}
		fmt.Print("Relative file path (empty to finish): ")
		line, _ := r.ReadString('\n')
		p := strings.TrimSpace(line)
		if p == "" {
			break
		}
		if err := validateRelativePath(p); err != nil {
			fmt.Printf("Invalid path: %v\n", err)
			continue
		}
		fmt.Println("Paste content, then enter |>--- CONTENT ---<| on its own line:")
		var body strings.Builder
		for {
			line, err := r.ReadString('\n')
			if err != nil && len(line) == 0 {
				return err
			}
			if strings.TrimRight(line, "\r\n") == "|>--- CONTENT ---<|" {
				break
			}
			body.WriteString(line)
		}
		if body.Len() == 0 {
			fmt.Println("Empty content: skipping file entry.")
			continue
		}
		doc.directories[0].files = append(doc.directories[0].files, docFile{path: filepath.ToSlash(p), content: []byte(body.String())})
	}
	return os.WriteFile(output, renderExpanded(doc), 0644)
}

func runUnpack(ctx context.Context, file, root, subpath string, options ...bool) error {
	allowAbsolute, fixInPlace := false, false
	if len(options) > 0 {
		allowAbsolute = options[0]
	}
	if len(options) > 1 {
		fixInPlace = options[1]
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	doc, ds := parseDocument(raw)
	if len(ds) > 0 {
		printDiagnostics(file, ds)
		fmt.Println("Validation failed; trying automatic fix...")
		fixed, notes, changed := fixDocument(raw)
		if !changed {
			return errors.New("document cannot be fixed safely")
		}
		doc, ds = parseDocument(fixed)
		if len(ds) > 0 {
			printDiagnostics(file, ds)
			return errors.New("automatic fix failed")
		}
		printDiagnostics(file, notes)
		fmt.Println("Automatic fix succeeded.")
		if fixInPlace {
			if err := os.WriteFile(file, fixed, 0644); err != nil {
				return err
			}
		}
	} else {
		fmt.Println("Validation succeeded.")
	}
	plan, err := buildUnpackPlan(doc, root, subpath, allowAbsolute)
	if err != nil {
		return err
	}
	fmt.Printf("Phase 1 complete: directories %d/%d, files %d/%d\n", len(plan.dirs), len(plan.dirs), len(plan.files), len(plan.files))
	for i, dir := range plan.dirs {
		select {
		case <-ctx.Done():
			return errors.New("unpacking interrupted")
		default:
		}
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		fmt.Printf("Directories: %d/%d\n", i+1, len(plan.dirs))
	}
	bar := progressbar.Default(int64(len(plan.files)), "Unpacking files")
	for _, f := range plan.files {
		select {
		case <-ctx.Done():
			return errors.New("unpacking interrupted; completed files were preserved")
		default:
		}
		if err := writeAtomically(ctx, f.path, f.data); err != nil {
			return err
		}
		_ = bar.Add(1)
	}
	return nil
}
func buildUnpackPlan(doc document, root, subpath string, allowAbsolute bool) (unpackPlan, error) {
	var p unpackPlan
	dirs := map[string]struct{}{}
	for _, d := range doc.directories {
		if !matchesSubpath(d.path, subpath) {
			continue
		}
		outDir, err := destinationDir(root, d.path, allowAbsolute)
		if err != nil {
			return p, fmt.Errorf("directory %q: %w", d.path, err)
		}
		dirs[outDir] = struct{}{}
		for _, f := range d.files {
			outFile, err := destinationFile(outDir, f.path, allowAbsolute)
			if err != nil {
				return p, fmt.Errorf("file %q: %w", f.path, err)
			}
			data, err := decodedContent(f)
			if err != nil {
				return p, err
			}
			dirs[filepath.Dir(outFile)] = struct{}{}
			p.files = append(p.files, unpackFile{outFile, data})
		}
	}
	for d := range dirs {
		p.dirs = append(p.dirs, d)
	}
	sort.Strings(p.dirs)
	return p, nil
}
func destinationDir(root, p string, allow bool) (string, error) {
	if filepath.IsAbs(p) {
		if !allow {
			return "", errors.New("absolute path requires --allow-absolute-paths")
		}
		return filepath.Clean(p), nil
	}
	if err := validateRelativePath(p); err != nil {
		return "", err
	}
	return filepath.Join(root, filepath.FromSlash(p)), nil
}
func destinationFile(dir, p string, allow bool) (string, error) {
	if filepath.IsAbs(p) {
		if !allow {
			return "", errors.New("absolute path requires --allow-absolute-paths")
		}
		return filepath.Clean(p), nil
	}
	if err := validateRelativePath(p); err != nil {
		return "", err
	}
	return filepath.Join(dir, filepath.FromSlash(p)), nil
}
func validateRelativePath(p string) error {
	n := strings.ReplaceAll(p, "\\", "/")
	if n == "" || filepath.IsAbs(p) || pathpkg.IsAbs(n) {
		return errors.New("path must be relative")
	}
	c := pathpkg.Clean(n)
	if c == "." || c == ".." || strings.HasPrefix(c, "../") {
		return errors.New("path must not contain traversal")
	}
	return nil
}
func writeAtomically(ctx context.Context, target string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".documax-unpack-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = tmp.Close(); _ = os.Remove(name) }()
	if _, err = tmp.Write(data); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return errors.New("unpacking interrupted while writing file")
	default:
	}
	return os.Rename(name, target)
}
func matchesSubpath(target, subpath string) bool {
	if subpath == "" {
		return true
	}
	a := strings.Split(pathpkg.Clean(filepath.ToSlash(target)), "/")
	b := strings.Split(pathpkg.Clean(filepath.ToSlash(subpath)), "/")
	for i := 0; i+len(b) <= len(a); i++ {
		if strings.Join(a[i:i+len(b)], "/") == strings.Join(b, "/") {
			return true
		}
	}
	return false
}
func buildGitIgnore(base string) gitignore.Matcher {
	var patterns []gitignore.Pattern
	for _, name := range []string{".gitignore", ".documax.ignore"} {
		f, err := os.Open(filepath.Join(base, name))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			s := strings.TrimSpace(sc.Text())
			if s != "" && !strings.HasPrefix(s, "#") {
				patterns = append(patterns, gitignore.ParsePattern(s, nil))
			}
		}
		// Scanner errors leave the usable patterns intact. Packing will still
		// surface file-system errors while walking the source directory.
		_ = f.Close()
	}
	return gitignore.NewMatcher(patterns)
}
func setupSignalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() { <-ch; fmt.Println("\nInterrupt received; stopping safely..."); cancel() }()
	return ctx, cancel
}

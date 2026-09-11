package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
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
	"github.com/spf13/cobra"
)

const (
	DirStartPrefix  = "|<--- DIRECTORY=\""
	DirStartSuffix  = "\" --->|"
	DirEndMarker    = "|>--- DIRECTORY ---<|"
	FileStartPrefix = "|<--- FILE=\""
	FileEndMarker   = "|>--- FILE ---<|"
	EscapedMeta     = ";ESCAPED;"
	DefaultDocFile  = "documax-output.txt"
)

var (
	tagRE         = regexp.MustCompile(`\|<--- DIRECTORY="([^"]+)" --->\||\|<--- FILE="([^"]+)"(?:(;ESCAPED;)|;ENCODED="([^"]+)";)? --->\||\|>--- FILE ---<\||\|>--- DIRECTORY ---<\|`)
	indentedTagRE = regexp.MustCompile(`(?m)^[\t ]+\|(?:<--- (?:DIRECTORY|FILE)=|>--- (?:DIRECTORY|FILE))`)
)

type diagnostic struct {
	line    int
	message string
}
type docFile struct {
	path     string
	line     int
	content  []byte
	escaped  bool
	encoding string
}
type docDirectory struct {
	path  string
	line  int
	files []docFile
}
type document struct{ directories []docDirectory }
type tag struct {
	kind             string
	start, end, line int
}

func main() {
	root := &cobra.Command{Use: "documax", Short: "Documax - multi-document packaging utility"}
	root.AddCommand(packCmd(), unpackCmd(), validateCmd(), fixCmd(), minifyCmd(), expandCmd())
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func packCmd() *cobra.Command {
	var dir, output string
	var interactive bool
	cmd := &cobra.Command{Use: "pack", RunE: func(_ *cobra.Command, _ []string) error {
		ctx, cancel := setupSignalContext()
		defer cancel()
		if interactive {
			return runPackInteractive(ctx, dir, output)
		}
		if dir == "" {
			return errors.New("missing --dir flag for directory packing")
		}
		return runPackDir(ctx, dir, output)
	}}
	cmd.Flags().StringVarP(&dir, "dir", "d", "", "Directory to pack")
	cmd.Flags().StringVarP(&output, "output", "o", DefaultDocFile, "Output Documax file path")
	cmd.Flags().BoolVar(&interactive, "from-clipboard", false, "Read pasted content until the content terminator")
	return cmd
}

func unpackCmd() *cobra.Command {
	var dir, subpath string
	var allowAbsolute, fixInPlace bool
	cmd := &cobra.Command{Use: "unpack [documax-file]", Args: cobra.MaximumNArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		ctx, cancel := setupSignalContext()
		defer cancel()
		input := DefaultDocFile
		if len(args) == 1 {
			input = args[0]
		}
		if _, err := os.Stat(input); err != nil {
			return fmt.Errorf("cannot read %q: %w", input, err)
		}
		if dir == "" {
			dir, _ = os.Getwd()
		}
		return runUnpack(ctx, input, dir, subpath, allowAbsolute, fixInPlace)
	}}
	cmd.Flags().StringVarP(&dir, "dir", "d", "", "Target root directory")
	cmd.Flags().StringVarP(&subpath, "subpath", "s", "", "Directory subpath to extract")
	cmd.Flags().BoolVar(&allowAbsolute, "allow-absolute-paths", false, "Allow absolute paths embedded in the document")
	cmd.Flags().BoolVar(&fixInPlace, "fix-in-place", false, "Save automatic repairs to the input document")
	return cmd
}

func validateCmd() *cobra.Command {
	return &cobra.Command{Use: "validate <documax-file>", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		raw, err := os.ReadFile(args[0])
		if err != nil {
			return err
		}
		_, ds := parseDocument(raw)
		if len(ds) == 0 {
			fmt.Printf("%s: valid Documax format\n", args[0])
			return nil
		}
		printDiagnostics(args[0], ds)
		return errors.New("document failed validation")
	}}
}

func fixCmd() *cobra.Command {
	var inPlace bool
	cmd := &cobra.Command{Use: "fix <documax-file>", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error { return runFix(args[0], inPlace) }}
	cmd.Flags().BoolVarP(&inPlace, "in-place", "i", false, "Overwrite the input file")
	return cmd
}

func minifyCmd() *cobra.Command {
	var output string
	cmd := &cobra.Command{Use: "minimize <documax-file>", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error { return runMinify(args[0], output) }}
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output file (defaults to stdout)")
	return cmd
}

func expandCmd() *cobra.Command {
	var output string
	cmd := &cobra.Command{Use: "expand <documax-file>", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error { return runExpand(args[0], output) }}
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output file (defaults to stdout)")
	return cmd
}

func parseDocument(raw []byte) (document, []diagnostic) {
	var doc document
	var ds []diagnostic
	for _, pos := range indentedTagRE.FindAllIndex(raw, -1) {
		ds = append(ds, diagnostic{lineAt(raw, pos[0]), "syntax tag must not be indented"})
	}
	matches := tagRE.FindAllSubmatchIndex(raw, -1)
	var currentDir *docDirectory
	var currentFile *docFile
	contentStart := 0
	for _, m := range matches {
		t := tag{start: m[0], end: m[1], line: lineAt(raw, m[0])}
		switch {
		case m[2] >= 0:
			t.kind = "dir-start"
		case m[4] >= 0:
			t.kind = "file-start"
		case bytes.Equal(raw[m[0]:m[1]], []byte(FileEndMarker)):
			t.kind = "file-end"
		default:
			t.kind = "dir-end"
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
			doc.directories = append(doc.directories, docDirectory{path: string(raw[m[2]:m[3]]), line: t.line})
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
			f := docFile{path: string(raw[m[4]:m[5]]), line: t.line, escaped: m[6] >= 0}
			if m[8] >= 0 {
				f.encoding = string(raw[m[8]:m[9]])
			}
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
			if currentFile.encoding != "" && currentFile.encoding != "BASE64" {
				ds = append(ds, diagnostic{currentFile.line, fmt.Sprintf("unsupported encoding %q", currentFile.encoding)})
			}
			if currentFile.encoding == "BASE64" {
				if _, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(body))); err != nil {
					ds = append(ds, diagnostic{currentFile.line, "invalid BASE64 file content"})
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

func fixDocument(raw []byte) ([]byte, []diagnostic, bool) {
	fixed := indentedTagRE.ReplaceAllFunc(raw, func(match []byte) []byte {
		return bytes.TrimLeft(match, " \t")
	})
	changed := !bytes.Equal(raw, fixed)
	var notes []diagnostic
	if changed {
		notes = append(notes, diagnostic{1, "removed indentation from syntax tags"})
	}
	ms := tagRE.FindAllIndex(fixed, -1)
	inDir, inFile := false, false
	inserts := map[int]string{}
	addEnd := func(pos, line int) {
		if _, ok := inserts[pos]; ok {
			return
		}
		s := FileEndMarker
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
	for _, m := range ms {
		text := string(fixed[m[0]:m[1]])
		switch {
		case strings.HasPrefix(text, DirStartPrefix):
			if inFile {
				addEnd(m[0], lineAt(fixed, m[0]))
				inFile = false
			}
			inDir = true
		case text == DirEndMarker:
			if inFile {
				addEnd(m[0], lineAt(fixed, m[0]))
				inFile = false
			}
			inDir = false
		case strings.HasPrefix(text, FileStartPrefix):
			if inDir {
				inFile = true
			}
		case text == FileEndMarker:
			inFile = false
		}
	}
	if inFile {
		addEnd(len(fixed), lineAt(fixed, len(fixed)))
		inFile = false
	}
	if inDir {
		prefix := ""
		if len(fixed) > 0 && fixed[len(fixed)-1] != '\n' {
			prefix = "\n"
		}
		inserts[len(fixed)] += prefix + DirEndMarker + "\n"
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
	if f.encoding == "BASE64" {
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
		if err != nil {
			return nil, err
		}
		data = decoded
	}
	if f.escaped {
		data = []byte(unescapeMarkers(string(data)))
	}
	return data, nil
}
func directoryHeader(p string) string { return DirStartPrefix + p + DirStartSuffix }
func fileHeader(p string, escaped bool, encoded bool) string {
	if encoded {
		return FileStartPrefix + p + "\";ENCODED=\"BASE64\"; --->|"
	}
	if escaped {
		return FileStartPrefix + p + EscapedMeta + "\" --->|"
	}
	return FileStartPrefix + p + "\" --->|"
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
		out.WriteString(directoryHeader(d.path))
		for _, f := range d.files {
			data, err := decodedContent(f)
			if err != nil {
				return err
			}
			out.WriteString(fileHeader(f.path, false, true))
			out.WriteString(base64.StdEncoding.EncodeToString(data))
			out.WriteString(FileEndMarker)
		}
		out.WriteString(DirEndMarker)
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
		out.WriteString(directoryHeader(d.path) + "\n")
		for _, f := range d.files {
			data, err := decodedContent(f)
			if err != nil {
				continue
			}
			body, escaped := escapeMarkers(string(data))
			out.WriteString(fileHeader(f.path, escaped, false) + "\n" + body)
			if !strings.HasSuffix(body, "\n") {
				out.WriteByte('\n')
			}
			out.WriteString(FileEndMarker + "\n")
		}
		out.WriteString(DirEndMarker + "\n")
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

type packItem struct{ rel, full string }

func runPackDir(ctx context.Context, source, output string) error {
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
	defer func() { tmp.Close(); os.Remove(tmpName) }()
	w := bufio.NewWriter(tmp)
	root := filepath.Base(abs) + "/"
	if _, err = w.WriteString(directoryHeader(root) + "\n"); err != nil {
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
		body, escaped := escapeMarkers(string(data))
		if _, err = w.WriteString(fileHeader(item.rel, escaped, false) + "\n" + body); err != nil {
			return err
		}
		if !strings.HasSuffix(body, "\n") {
			if _, err = w.WriteString("\n"); err != nil {
				return err
			}
		}
		if _, err = w.WriteString(FileEndMarker + "\n"); err != nil {
			return err
		}
		_ = bar.Add(1)
	}
	if _, err = w.WriteString(DirEndMarker + "\n"); err != nil {
		return err
	}
	for _, dir := range emptyDirs {
		if _, err = w.WriteString(directoryHeader(root+dir+"/") + "\n" + DirEndMarker + "\n"); err != nil {
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

func runPackInteractive(ctx context.Context, scope, output string) error {
	r := bufio.NewReader(os.Stdin)
	if scope == "" {
		fmt.Print("Enter base directory path: ")
		line, _ := r.ReadString('\n')
		scope = strings.TrimSpace(line)
		if scope == "" {
			return errors.New("empty directory path: exiting")
		}
	}
	doc := document{directories: []docDirectory{{path: filepath.ToSlash(scope)}}}
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

type unpackFile struct {
	path string
	data []byte
}
type unpackPlan struct {
	dirs  []string
	files []unpackFile
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
	defer func() { tmp.Close(); os.Remove(name) }()
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
func escapeMarkers(s string) (string, bool) {
	changed := false
	if strings.Contains(s, "|<---") {
		s = strings.ReplaceAll(s, "|<---", `\|\<---`)
		changed = true
	}
	if strings.Contains(s, "|>---") {
		s = strings.ReplaceAll(s, "|>---", `\|\>---`)
		changed = true
	}
	return s, changed
}
func unescapeMarkers(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, `\|\<---`, "|<---"), `\|\>---`, "|>---")
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
		f.Close()
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

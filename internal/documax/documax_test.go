package documax

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGzipBase64RoundTripPreservesPythonBytes(t *testing.T) {
	temp := t.TempDir()
	original := filepath.Join(temp, "original.txt")
	minimized := filepath.Join(temp, "minimized.txt")
	expanded := filepath.Join(temp, "expanded.txt")
	python := "def greet():\n    message = 'hello'\n\n    if message:\n        print(message)\n"
	doc := "[DIR: project/]\n[FILE: src/main.py]\n" + python + "[/FILE]\n[/DIR]\n"
	if err := os.WriteFile(original, []byte(doc), 0644); err != nil {
		t.Fatal(err)
	}
	if err := runMinify(original, minimized); err != nil {
		t.Fatal(err)
	}
	minBytes, err := os.ReadFile(minimized)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(minBytes, []byte("[FILE: src/main.py;ENC=GZ+B64]")) {
		t.Fatalf("missing GZ+B64 metadata: %s", minBytes)
	}
	if ok, errs := validateDocumax(minimized); !ok {
		t.Fatalf("minimized document invalid: %v", errs)
	}
	if err := runExpand(minimized, expanded); err != nil {
		t.Fatal(err)
	}
	expandedBytes, err := os.ReadFile(expanded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(expandedBytes, []byte(python)) {
		t.Fatalf("Python content changed:\n%s", expandedBytes)
	}
}

func TestPackMinimizedDirectly(t *testing.T) {
	source := t.TempDir()
	output := filepath.Join(t.TempDir(), "archive.min.txt")
	if err := os.WriteFile(filepath.Join(source, "main.py"), []byte("if True:\n    print('packed')\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := runPackMinimized(context.Background(), source, output, formatBracket, false); err != nil {
		t.Fatal(err)
	}
	packed, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(packed, []byte(";ENC=GZ+B64]")) {
		t.Fatalf("direct pack did not minimize output: %s", packed)
	}
}

func TestXMLPackingExpansionAndUnpacking(t *testing.T) {
	source := t.TempDir()
	target := t.TempDir()
	output := filepath.Join(t.TempDir(), "archive.xml.txt")
	if err := os.MkdirAll(filepath.Join(source, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "src", "app.py"), []byte("if True:\n    print('xml')\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := runPackDir(context.Background(), source, output, formatXML); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("<d:dir path=\"")) || !bytes.Contains(raw, []byte("<d:file path=\"src/app.py\">")) {
		t.Fatalf("XML tags missing: %s", raw)
	}
	if ok, errs := validateDocumax(output); !ok {
		t.Fatalf("XML document invalid: %v", errs)
	}
	minimized := filepath.Join(t.TempDir(), "archive.min.xml.txt")
	expanded := filepath.Join(t.TempDir(), "archive.expanded.xml.txt")
	minimizedTarget := t.TempDir()
	if err := runMinify(output, minimized); err != nil {
		t.Fatal(err)
	}
	if err := runExpand(minimized, expanded); err != nil {
		t.Fatal(err)
	}
	expandedBytes, err := os.ReadFile(expanded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(expandedBytes, []byte("<d:file path=\"src/app.py\">")) {
		t.Fatalf("XML expansion changed the syntax: %s", expandedBytes)
	}
	if err := runUnpack(context.Background(), minimized, minimizedTarget, ""); err != nil {
		t.Fatal(err)
	}
	minimizedRoundTrip, err := os.ReadFile(filepath.Join(minimizedTarget, filepath.Base(source), "src", "app.py"))
	if err != nil {
		t.Fatal(err)
	}
	if string(minimizedRoundTrip) != "if True:\n    print('xml')\n" {
		t.Fatalf("direct minimized unpack changed content: %q", minimizedRoundTrip)
	}
	if err := runUnpack(context.Background(), output, target, ""); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(target, filepath.Base(source), "src", "app.py"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "if True:\n    print('xml')\n" {
		t.Fatalf("got %q", got)
	}
}

func TestSubpathMatching(t *testing.T) {
	for _, tc := range []struct {
		target, subpath string
		want            bool
	}{
		{"a/b/c", "c", true}, {"a/b/c/d/e", "c", true},
		{"a/b/d/e", "d/e", true}, {"a/b/c/d/e", "d/e", true},
		{"a/b/x", "c", false}, {"a/b/d/f", "d/e", false},
	} {
		if got := matchesSubpath(tc.target, tc.subpath); got != tc.want {
			t.Errorf("matchesSubpath(%q, %q) = %v", tc.target, tc.subpath, got)
		}
	}
}

func TestFixBracketDocument(t *testing.T) {
	file := filepath.Join(t.TempDir(), "broken.txt")
	content := "  [DIR: project/]\n[FILE: main.py]\n    print('ok')\n[/DIR]\n"
	if err := os.WriteFile(file, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if err := runFix(file, true); err != nil {
		t.Fatal(err)
	}
	if ok, errs := validateDocumax(file); !ok {
		t.Fatalf("fixed document invalid: %s", strings.Join(errs, "; "))
	}
}

func TestPackAlwaysExcludesGitMetadata(t *testing.T) {
	source := t.TempDir()
	output := filepath.Join(t.TempDir(), "archive.txt")
	if err := os.MkdirAll(filepath.Join(source, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(source, ".idea"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(source, "target"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, ".git", "index"), []byte("git metadata"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, ".DS_Store"), []byte("finder metadata"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, ".idea", "workspace.xml"), []byte("ide metadata"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "target", "generated.txt"), []byte("build output"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "keep.txt"), []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := runPackDir(context.Background(), source, output); err != nil {
		t.Fatal(err)
	}
	packed, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	for _, unwanted := range []string{".git/index", "git metadata", ".DS_Store", "finder metadata", ".idea/workspace.xml", "ide metadata", "target/generated.txt", "build output"} {
		if bytes.Contains(packed, []byte(unwanted)) {
			t.Fatalf("default-excluded path was packed: %s", unwanted)
		}
	}
}

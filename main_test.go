package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidationAndFix(t *testing.T) {
	tempDir := t.TempDir()
	validDoc := filepath.Join(tempDir, "valid.txt")
	invalidDoc := filepath.Join(tempDir, "invalid.txt")

	validContent := `|<--- DIRECTORY="myproject/" --->|
|<--- FILE="a/b/c/y.java" --->|
public class Main {
    int x = 10;
}
|>--- FILE ---<|
|>--- DIRECTORY ---<|`

	if err := os.WriteFile(validDoc, []byte(validContent), 0644); err != nil {
		t.Fatal(err)
	}

	ok, errs := validateDocumax(validDoc)
	if !ok || len(errs) > 0 {
		t.Fatalf("expected valid file, got errors: %v", errs)
	}

	// Indented tag failure
	invalidContent := `  |<--- DIRECTORY="myproject/" --->|
|<--- FILE="a/b/c/y.java" --->|
code
|>--- FILE ---<|
|>--- DIRECTORY ---<|`

	if err := os.WriteFile(invalidDoc, []byte(invalidContent), 0644); err != nil {
		t.Fatal(err)
	}

	ok, errs = validateDocumax(invalidDoc)
	if ok || len(errs) == 0 {
		t.Fatal("expected failure due to indented tag")
	}

	// Test Fixer
	if err := runFix(invalidDoc, true); err != nil {
		t.Fatalf("fix failed: %v", err)
	}

	ok, errs = validateDocumax(invalidDoc)
	if !ok || len(errs) > 0 {
		t.Fatalf("expected fixed file to be valid, got: %v", errs)
	}
}

func TestPackUnpackRoundtrip(t *testing.T) {
	sourceDir := t.TempDir()
	unpackDir := t.TempDir()
	docFile := filepath.Join(t.TempDir(), "archive.txt")

	// Structure to pack
	os.MkdirAll(filepath.Join(sourceDir, "src", "main"), 0755)
	sourceFile1 := filepath.Join(sourceDir, "src", "main", "App.java")
	sourceFile2 := filepath.Join(sourceDir, "pom.xml")
	ignoreFile := filepath.Join(sourceDir, ".documax.ignore")
	shouldBeIgnored := filepath.Join(sourceDir, "secret.key")

	os.WriteFile(sourceFile1, []byte("package main;\n\npublic class App {\n    // indent\n}\n"), 0644)
	os.WriteFile(sourceFile2, []byte("<project>\n    <modelVersion>4.0.0</modelVersion>\n</project>\n"), 0644)
	os.WriteFile(ignoreFile, []byte("*.key\n"), 0644)
	os.WriteFile(shouldBeIgnored, []byte("secret"), 0644)

	ctx := context.Background()

	// Pack
	if err := runPackDir(ctx, sourceDir, docFile); err != nil {
		t.Fatalf("packing failed: %v", err)
	}

	// Unpack
	if err := runUnpack(ctx, docFile, unpackDir, ""); err != nil {
		t.Fatalf("unpacking failed: %v", err)
	}

	// Verify unpacked files
	baseName := filepath.Base(sourceDir)
	unpackedApp := filepath.Join(unpackDir, baseName, "src", "main", "App.java")
	appBytes, err := os.ReadFile(unpackedApp)
	if err != nil {
		t.Fatalf("failed reading unpacked app: %v", err)
	}

	if !strings.Contains(string(appBytes), "    // indent") {
		t.Fatal("indentation was not preserved in unpacked file")
	}

	// Verify .documax.ignore effect
	ignoredTarget := filepath.Join(unpackDir, baseName, "secret.key")
	if _, err := os.Stat(ignoredTarget); !os.IsNotExist(err) {
		t.Fatal("ignored file was erroneously unpacked")
	}
}

func TestSubpathMatching(t *testing.T) {
	cases := []struct {
		target   string
		subpath  string
		expected bool
	}{
		{"a/b/c", "c", true},
		{"a/b/c/d/e", "c", true},
		{"a/b/d/e", "d/e", true},
		{"a/b/c/d/e", "d/e", true},
		{"a/b/x", "c", false},
		{"a/b/d/f", "d/e", false},
	}

	for _, c := range cases {
		res := matchesSubpath(c.target, c.subpath)
		if res != c.expected {
			t.Errorf("matchesSubpath(%q, %q) = %v; want %v", c.target, c.subpath, res, c.expected)
		}
	}
}

func TestMarkerEscaping(t *testing.T) {
	contentWithMarkers := "Here is boundary:\n|<--- FILE=\"inner.txt\" --->|\ncontent\n|>--- FILE ---<|"
	escaped, isEsc := escapeMarkers(contentWithMarkers)
	if !isEsc {
		t.Fatal("expected content to be marked as escaped")
	}

	unescaped := unescapeMarkers(escaped)
	if unescaped != contentWithMarkers {
		t.Fatalf("roundtrip escaping failed:\nGot:\n%s\nWant:\n%s", unescaped, contentWithMarkers)
	}
}

func TestMinifyAndExpand(t *testing.T) {
	tempDir := t.TempDir()
	original := filepath.Join(tempDir, "orig.txt")
	minified := filepath.Join(tempDir, "min.txt")
	expanded := filepath.Join(tempDir, "exp.txt")

	data := `|<--- DIRECTORY="proj/" --->|
|<--- FILE="nested/code.py" --->|
def test():
    return True
|>--- FILE ---<|
|>--- DIRECTORY ---<|`

	os.WriteFile(original, []byte(data), 0644)

	if err := runMinify(original, minified); err != nil {
		t.Fatalf("minify failed: %v", err)
	}

	minBytes, err := os.ReadFile(minified)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(minBytes), `|<--- DIRECTORY="proj/" --->||<--- FILE="nested/code.py";ENCODED="BASE64"; --->|`) {
		t.Fatalf("minify did not collapse tags properly: %s", string(minBytes))
	}
	if ok, errs := validateDocumax(minified); !ok {
		t.Fatalf("minimized output is not valid documax: %v", errs)
	}

	if err := runExpand(minified, expanded); err != nil {
		t.Fatalf("expand failed: %v", err)
	}

	ok, errs := validateDocumax(expanded)
	if !ok {
		t.Fatalf("expanded output is not valid documax: %v", errs)
	}
	expandedBytes, err := os.ReadFile(expanded)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(expandedBytes), "    return True") {
		t.Fatal("base64 expansion did not preserve Python indentation")
	}
}

func TestFixAddsFileClosingTagBeforeDirectoryEnd(t *testing.T) {
	tempDir := t.TempDir()
	file := filepath.Join(tempDir, "missing-end.txt")
	content := `|<--- DIRECTORY="project/" --->|
|<--- FILE="main.py" --->|
if True:
    print("preserved")
|>--- DIRECTORY ---<|`
	if err := os.WriteFile(file, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if err := runFix(file, true); err != nil {
		t.Fatal(err)
	}
	if ok, errs := validateDocumax(file); !ok {
		t.Fatalf("fixed document is invalid: %v", errs)
	}
}

func TestUnpackCreatesEmptyDirectoryAndRejectsTraversal(t *testing.T) {
	tempDir := t.TempDir()
	doc := filepath.Join(tempDir, "empty.txt")
	if err := os.WriteFile(doc, []byte(`|<--- DIRECTORY="project/empty/" --->|
|>--- DIRECTORY ---<|`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := runUnpack(context.Background(), doc, tempDir, ""); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(tempDir, "project", "empty")); err != nil || !info.IsDir() {
		t.Fatal("empty directory was not created")
	}

	bad := filepath.Join(tempDir, "bad.txt")
	if err := os.WriteFile(bad, []byte(`|<--- DIRECTORY="../outside/" --->|
|>--- DIRECTORY ---<|`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := runUnpack(context.Background(), bad, tempDir, ""); err == nil {
		t.Fatal("expected traversal path to be rejected")
	}
}

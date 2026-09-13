package documax

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveDocumentInput(t *testing.T) {
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	workingDirectory := t.TempDir()
	if err := os.Chdir(workingDirectory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })

	if _, err := resolveDocumentInput(nil); err == nil || !strings.Contains(err.Error(), "could not find") {
		t.Fatalf("missing defaults error = %v", err)
	}
	if err := os.WriteFile("documax-output.md", []byte("document"), 0644); err != nil {
		t.Fatal(err)
	}
	input, err := resolveDocumentInput(nil)
	if err != nil || input != "documax-output.md" {
		t.Fatalf("resolve markdown = %q, %v", input, err)
	}
	if err := os.WriteFile("documax-output.xml", []byte("document"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveDocumentInput(nil); err == nil || !strings.Contains(err.Error(), "both") {
		t.Fatalf("both defaults error = %v", err)
	}
}

func TestResolvePackOutput(t *testing.T) {
	source := filepath.Join("/tmp", "My Project")
	tests := []struct {
		name, requested, format string
		currentDirectoryDefault bool
		want                    string
	}{
		{"current bracket", "", "bracket", true, "documax-output.md"},
		{"current xml", "", "xml", true, "documax-output.xml"},
		{"selected bracket", "", "bracket", false, "My-Project-documax.md"},
		{"selected xml", "", "xml", false, "My-Project-documax.xml"},
		{"output directory", "output directory", "xml", false, filepath.Join("output directory", "My-Project", "documax-output.xml")},
		{"output filename", "output directory/archive.txt", "bracket", false, "output directory/archive.txt"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := resolvePackOutput(source, test.requested, test.format, test.currentDirectoryDefault)
			if err != nil || got != test.want {
				t.Fatalf("resolvePackOutput() = %q, %v; want %q", got, err, test.want)
			}
		})
	}
	if _, err := resolvePackOutput(source, "archive.json", "bracket", false); err == nil || !strings.Contains(err.Error(), ".json") {
		t.Fatalf("unsupported extension error = %v", err)
	}
}

func TestPackCommandAcceptsPositionalDirectory(t *testing.T) {
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	workingDirectory := t.TempDir()
	if err := os.Chdir(workingDirectory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })

	source := "My Project"
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "main.py"), []byte("if True:\n    print('ok')\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := packCmd()
	cmd.SetArgs([]string{source, "--minimized"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	output := "My-Project-documax.md"
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("expected positional pack output %q: %v", output, err)
	}
}

func TestPackCommandRejectsDuplicateDirectoryInputs(t *testing.T) {
	cmd := packCmd()
	cmd.SetArgs([]string{"source", "--dir", "source"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "either as an argument or with --dir") {
		t.Fatalf("duplicate directory error = %v", err)
	}
}

func TestCommandsEndToEnd(t *testing.T) {
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	workingDirectory := t.TempDir()
	if err := os.Chdir(workingDirectory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })

	document := "[DIR: project/]\n[FILE: main.py]\nif True:\n    print('ok')\n[/FILE]\n[/DIR]\n"
	if err := os.WriteFile("documax-output.md", []byte(document), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"validate", "documax-output.md"}, {"minimize"}, {"expand"}, {"unpack", "--dir", "restored"}} {
		cmd := NewRootCmd()
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("documax %s: %v", strings.Join(args, " "), err)
		}
	}
	if _, err := os.Stat(filepath.Join("restored", "project", "main.py")); err != nil {
		t.Fatal(err)
	}

	broken := "[DIR: project/]\n[FILE: main.py]\nprint('fixed')\n[/DIR]\n"
	if err := os.WriteFile("broken.md", []byte(broken), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := NewRootCmd()
	cmd.SetArgs([]string{"fix", "--in-place", "broken.md"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestConfirmPackOverwrite(t *testing.T) {
	file := filepath.Join(t.TempDir(), "output.md")
	if err := os.WriteFile(file, []byte("existing"), 0644); err != nil {
		t.Fatal(err)
	}
	var prompt bytes.Buffer
	if err := confirmPackOverwriteWithReader(file, strings.NewReader("yes\n"), &prompt); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt.String(), "Overwrite?") {
		t.Fatalf("confirmation prompt = %q", prompt.String())
	}
	if err := confirmPackOverwriteWithReader(file, strings.NewReader("no\n"), &bytes.Buffer{}); err == nil {
		t.Fatal("expected overwrite rejection")
	}
	if err := os.WriteFile(file, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := confirmPackOverwriteWithReader(file, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("empty output should not require confirmation: %v", err)
	}
}

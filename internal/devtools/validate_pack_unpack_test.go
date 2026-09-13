package devtools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunValidatePackUnpack(t *testing.T) {
	parent := t.TempDir()
	source := filepath.Join(parent, "source")
	if err := os.MkdirAll(filepath.Join(source, "empty"), 0755); err != nil {
		t.Fatal(err)
	}
	python := "def greet():\n    print('hello')\n"
	if err := os.WriteFile(filepath.Join(source, "main.py"), []byte(python), 0644); err != nil {
		t.Fatal(err)
	}
	if err := runValidatePackUnpack(context.Background(), source, false); err != nil {
		t.Fatal(err)
	}
}

func TestValidatePackUnpackCommand(t *testing.T) {
	cmd := NewRootCmd()
	cmd.SetArgs([]string{"validate-pack-unpack"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "missing --dir flag") {
		t.Fatalf("missing directory error = %v", err)
	}
}

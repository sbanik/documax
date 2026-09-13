// Package devtools provides development-only Documax verification commands.
package devtools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"documax/internal/core"

	"github.com/spf13/cobra"
)

// NewRootCmd creates the developer-only command-line application.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{Use: "documax-dev", Short: "Documax developer tools"}
	root.AddCommand(validatePackUnpackCmd())
	return root
}

func validatePackUnpackCmd() *cobra.Command {
	var source string
	var keepArtifacts bool

	cmd := &cobra.Command{
		Use:   "validate-pack-unpack",
		Short: "Pack, unpack, and compare a directory using Documax rules",
		RunE: func(_ *cobra.Command, _ []string) error {
			if source == "" {
				return errors.New("missing --dir flag")
			}
			ctx, cancel := core.NewSignalContext()
			defer cancel()
			return runValidatePackUnpack(ctx, source, keepArtifacts)
		},
	}
	cmd.Flags().StringVarP(&source, "dir", "d", "", "Source directory to validate")
	cmd.Flags().BoolVar(&keepArtifacts, "keep-artifacts", false, "Keep the temporary archive and unpacked directory")
	return cmd
}

func runValidatePackUnpack(ctx context.Context, source string, keepArtifacts bool) error {
	absSource, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	info, err := os.Stat(absSource)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%q is not a directory", source)
	}

	parent := filepath.Dir(absSource)
	base := filepath.Base(absSource)
	archive, err := os.CreateTemp(parent, "."+base+"-documax-validate-*.txt")
	if err != nil {
		return err
	}
	archivePath := archive.Name()
	if err := archive.Close(); err != nil {
		return err
	}
	if err := os.Remove(archivePath); err != nil {
		return err
	}

	unpackRoot, err := os.MkdirTemp(parent, "."+base+"-documax-unpacked-")
	if err != nil {
		return err
	}
	if !keepArtifacts {
		defer func() { _ = os.Remove(archivePath) }()
		defer func() { _ = os.RemoveAll(unpackRoot) }()
	}

	fmt.Printf("Packing %s into %s\n", absSource, archivePath)
	if err := core.PackDirectory(ctx, absSource, archivePath, "bracket"); err != nil {
		return err
	}
	fmt.Printf("Unpacking into %s\n", unpackRoot)
	if err := core.Unpack(ctx, archivePath, unpackRoot, "", false, false); err != nil {
		return err
	}

	expected, err := core.CollectPackableTree(absSource)
	if err != nil {
		return err
	}
	actualRoot := filepath.Join(unpackRoot, base)
	actual, err := collectActualTree(actualRoot)
	if err != nil {
		return err
	}
	differences, err := compareTrees(absSource, actualRoot, expected, actual)
	if err != nil {
		return err
	}
	if len(differences) > 0 {
		for _, difference := range differences {
			fmt.Println(difference)
		}
		if keepArtifacts {
			fmt.Printf("Validation artifacts kept:\n  archive: %s\n  unpacked: %s\n", archivePath, unpackRoot)
		}
		return fmt.Errorf("pack/unpack validation failed with %d difference(s)", len(differences))
	}
	fmt.Printf("Pack/unpack validation passed: %d directories and %d files match.\n", countDirectories(expected), countFiles(expected))
	if keepArtifacts {
		fmt.Printf("Validation artifacts kept:\n  archive: %s\n  unpacked: %s\n", archivePath, unpackRoot)
	}
	return nil
}

func collectActualTree(root string) (map[string]core.TreeEntry, error) {
	entries := map[string]core.TreeEntry{}
	err := filepath.Walk(root, func(current string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		entries[filepath.ToSlash(rel)] = core.TreeEntry{IsDir: info.IsDir()}
		return nil
	})
	return entries, err
}

func compareTrees(sourceRoot, unpackedRoot string, expected, actual map[string]core.TreeEntry) ([]string, error) {
	var differences []string
	for rel, expectedEntry := range expected {
		actualEntry, exists := actual[rel]
		if !exists {
			differences = append(differences, "missing: "+rel)
			continue
		}
		if actualEntry.IsDir != expectedEntry.IsDir {
			differences = append(differences, "type mismatch: "+rel)
			continue
		}
		if expectedEntry.IsDir {
			continue
		}
		sourceData, err := os.ReadFile(filepath.Join(sourceRoot, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		unpackedData, err := os.ReadFile(filepath.Join(unpackedRoot, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(sourceData, unpackedData) {
			differences = append(differences, "content differs: "+rel)
		}
	}
	for rel := range actual {
		if _, exists := expected[rel]; !exists {
			differences = append(differences, "unexpected: "+rel)
		}
	}
	sort.Strings(differences)
	return differences, nil
}

func countDirectories(entries map[string]core.TreeEntry) int {
	count := 0
	for _, entry := range entries {
		if entry.IsDir {
			count++
		}
	}
	return count
}

func countFiles(entries map[string]core.TreeEntry) int {
	count := 0
	for _, entry := range entries {
		if !entry.IsDir {
			count++
		}
	}
	return count
}

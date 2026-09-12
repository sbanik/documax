//go:build devtools

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// validatePackUnpackCmd verifies that packing and unpacking a directory
// reproduces every packable directory and file. Temporary artifacts are
// siblings of the source directory, never children of it.
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
			ctx, cancel := setupSignalContext()
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
		defer os.Remove(archivePath)
		defer os.RemoveAll(unpackRoot)
	}

	fmt.Printf("Packing %s into %s\n", absSource, archivePath)
	if err := runPackDir(ctx, absSource, archivePath); err != nil {
		return err
	}
	fmt.Printf("Unpacking into %s\n", unpackRoot)
	if err := runUnpack(ctx, archivePath, unpackRoot, ""); err != nil {
		return err
	}

	expected, err := collectPackableTree(absSource)
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

type treeEntry struct {
	isDir bool
}

func collectPackableTree(root string) (map[string]treeEntry, error) {
	ignorer := buildGitIgnore(root)
	entries := map[string]treeEntry{}
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
			entries[rel] = treeEntry{isDir: true}
			return nil
		}
		if rel == ".documax.ignore" {
			return nil
		}
		if rel != ".gitignore" && ignorer.Match(strings.Split(rel, "/"), false) {
			return nil
		}
		entries[rel] = treeEntry{}
		return nil
	})
	return entries, err
}

func collectActualTree(root string) (map[string]treeEntry, error) {
	entries := map[string]treeEntry{}
	err := filepath.Walk(root, func(current string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		entries[filepath.ToSlash(rel)] = treeEntry{isDir: info.IsDir()}
		return nil
	})
	return entries, err
}

func compareTrees(sourceRoot, unpackedRoot string, expected, actual map[string]treeEntry) ([]string, error) {
	var differences []string
	for rel, expectedEntry := range expected {
		actualEntry, exists := actual[rel]
		if !exists {
			differences = append(differences, "missing: "+rel)
			continue
		}
		if actualEntry.isDir != expectedEntry.isDir {
			differences = append(differences, "type mismatch: "+rel)
			continue
		}
		if expectedEntry.isDir {
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

func countDirectories(entries map[string]treeEntry) int {
	count := 0
	for _, entry := range entries {
		if entry.isDir {
			count++
		}
	}
	return count
}

func countFiles(entries map[string]treeEntry) int {
	count := 0
	for _, entry := range entries {
		if !entry.isDir {
			count++
		}
	}
	return count
}

// Package documax defines the distributed Documax command-line interface.
package documax

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"documax/internal/core"

	"github.com/spf13/cobra"
)

// NewRootCmd creates the Documax command-line application.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{Use: "documax", Short: "Documax - multi-document packaging utility"}
	root.AddCommand(packCmd(), unpackCmd(), validateCmd(), fixCmd(), minimizeCmd(), expandCmd())
	return root
}

func packCmd() *cobra.Command {
	var dir, output, format string
	var interactive, minimized bool
	cmd := &cobra.Command{Use: "pack [directory]", Short: "Package a directory into a Documax document", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if format != "bracket" && format != "xml" {
			return errors.New("format must be bracket or xml")
		}
		directoryProvided := cmd.Flags().Changed("dir")
		if len(args) == 1 {
			if cmd.Flags().Changed("dir") {
				return errors.New("provide the directory either as an argument or with --dir, not both")
			}
			dir = args[0]
			directoryProvided = true
		}
		if dir == "" && !interactive {
			dir, _ = os.Getwd()
		}
		resolvedOutput, err := resolvePackOutput(dir, output, format, !directoryProvided)
		if err != nil {
			return err
		}
		if err := confirmPackOverwrite(resolvedOutput); err != nil {
			return err
		}
		ctx, cancel := core.NewSignalContext()
		defer cancel()
		if minimized {
			return core.PackMinimized(ctx, dir, resolvedOutput, format, interactive)
		}
		if interactive {
			return core.PackInteractive(ctx, dir, resolvedOutput, format)
		}
		return core.PackDirectory(ctx, dir, resolvedOutput, format)
	}}
	cmd.Flags().StringVarP(&dir, "dir", "d", "", "Directory to pack")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output file path or output directory")
	cmd.Flags().StringVarP(&format, "format", "f", "bracket", "Output format: bracket or xml")
	cmd.Flags().BoolVarP(&minimized, "minimized", "m", false, "Write a GZ+B64 minimized document directly")
	cmd.Flags().BoolVar(&interactive, "from-clipboard", false, "Read pasted content until the content terminator")
	return cmd
}

func unpackCmd() *cobra.Command {
	var dir, subpath string
	var allowAbsolute, fixInPlace bool
	cmd := &cobra.Command{
		Use:   "unpack [documax-file]",
		Short: "Extract files and directories from a Documax document",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			ctx, cancel := core.NewSignalContext()
			defer cancel()
			input, err := resolveDocumentInput(args)
			if err != nil {
				return err
			}
			if _, err := os.Stat(input); err != nil {
				return fmt.Errorf("cannot read %q: %w", input, err)
			}
			if dir == "" {
				dir, _ = os.Getwd()
			}
			return core.Unpack(ctx, input, dir, subpath, allowAbsolute, fixInPlace)
		}}
	cmd.Flags().StringVarP(&dir, "dir", "d", "", "Target root directory")
	cmd.Flags().StringVarP(&subpath, "subpath", "s", "", "Directory subpath to extract")
	cmd.Flags().BoolVar(&allowAbsolute, "allow-absolute-paths", false, "Allow absolute paths embedded in the document")
	cmd.Flags().BoolVar(&fixInPlace, "fix-in-place", false, "Save automatic repairs to the input document")
	return cmd
}

func validateCmd() *cobra.Command {
	return &cobra.Command{Use: "validate <documax-file>", Short: "Check a document for format and structural errors", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		valid, diagnostics := core.ValidateFile(args[0])
		if valid {
			fmt.Printf("%s: valid Documax format\n", args[0])
			return nil
		}
		for _, diagnostic := range diagnostics {
			fmt.Println(diagnostic)
		}
		return errors.New("document failed validation")
	}}
}

func fixCmd() *cobra.Command {
	var inPlace bool
	cmd := &cobra.Command{Use: "fix <documax-file>", Short: "Apply conservative repairs to a document", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error { return core.FixFile(args[0], inPlace) }}
	cmd.Flags().BoolVarP(&inPlace, "in-place", "i", false, "Overwrite the input file")
	return cmd
}

func minimizeCmd() *cobra.Command {
	var output string
	cmd := &cobra.Command{Use: "minimize [documax-file]", Short: "Compress file payloads as GZ+B64", Args: cobra.MaximumNArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		input, err := resolveDocumentInput(args)
		if err != nil {
			return err
		}
		return core.MinimizeFile(input, output)
	}}
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output file (defaults to overwriting the input)")
	return cmd
}

func expandCmd() *cobra.Command {
	var output string
	cmd := &cobra.Command{Use: "expand [documax-file]", Short: "Decode GZ+B64 payloads into readable source", Args: cobra.MaximumNArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		input, err := resolveDocumentInput(args)
		if err != nil {
			return err
		}
		return core.ExpandFile(input, output)
	}}
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output file (defaults to overwriting the input)")
	return cmd
}

func resolveDocumentInput(args []string) (string, error) {
	if len(args) == 1 {
		if _, err := os.Stat(args[0]); err != nil {
			return "", fmt.Errorf("cannot read %q: %w", args[0], err)
		}
		return args[0], nil
	}
	markdown, xml := "documax-output.md", "documax-output.xml"
	markdownExists := fileExists(markdown)
	xmlExists := fileExists(xml)
	switch {
	case markdownExists && xmlExists:
		return "", fmt.Errorf("both %q and %q were found; provide an explicit document filepath", markdown, xml)
	case markdownExists:
		return markdown, nil
	case xmlExists:
		return xml, nil
	default:
		return "", fmt.Errorf("could not find %q or %q; provide an explicit document filepath", markdown, xml)
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func resolvePackOutput(source, requested, format string, currentDirectoryDefault bool) (string, error) {
	extension := ".md"
	if format == "xml" {
		extension = ".xml"
	}
	if requested == "" {
		if currentDirectoryDefault {
			return "documax-output" + extension, nil
		}
		return selectedDirectoryName(source) + "-documax" + extension, nil
	}
	if ext := filepath.Ext(requested); ext != "" {
		switch strings.ToLower(ext) {
		case ".md", ".xml", ".txt":
			return requested, nil
		default:
			return "", fmt.Errorf("unsupported output extension %q: expected .md, .xml, or .txt", ext)
		}
	}
	return filepath.Join(requested, selectedDirectoryName(source), "documax-output"+extension), nil
}

func selectedDirectoryName(source string) string {
	name := filepath.Base(filepath.Clean(source))
	return strings.ReplaceAll(name, " ", "-")
}

func confirmPackOverwrite(output string) error {
	return confirmPackOverwriteWithReader(output, os.Stdin, os.Stderr)
}

func confirmPackOverwriteWithReader(output string, input io.Reader, prompt io.Writer) error {
	info, err := os.Stat(output)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("output path %q is a directory", output)
	}
	if info.Size() == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(prompt, "Output file %q already exists and is not empty. Overwrite? [y/N]: ", output); err != nil {
		return err
	}
	answer, err := bufio.NewReader(input).ReadString('\n')
	if err != nil && len(answer) == 0 {
		return fmt.Errorf("refusing to overwrite %q without confirmation", output)
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer != "y" && answer != "yes" {
		return fmt.Errorf("pack cancelled; %q was not overwritten", output)
	}
	return nil
}

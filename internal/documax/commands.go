// Package documax defines the distributed Documax command-line interface.
package documax

import (
	"errors"
	"fmt"
	"os"

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
	cmd := &cobra.Command{Use: "pack", Short: "Package a directory into a Documax document", RunE: func(_ *cobra.Command, _ []string) error {
		ctx, cancel := core.NewSignalContext()
		defer cancel()
		if minimized {
			return core.PackMinimized(ctx, dir, output, format, interactive)
		}
		if interactive {
			return core.PackInteractive(ctx, dir, output, format)
		}
		if dir == "" {
			return errors.New("missing --dir flag for directory packing")
		}
		return core.PackDirectory(ctx, dir, output, format)
	}}
	cmd.Flags().StringVarP(&dir, "dir", "d", "", "Directory to pack")
	cmd.Flags().StringVarP(&output, "output", "o", core.DefaultDocFile, "Output Documax file path")
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
			input := core.DefaultDocFile
			if len(args) == 1 {
				input = args[0]
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
	cmd := &cobra.Command{Use: "minimize <documax-file>", Short: "Compress file payloads as GZ+B64", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error { return core.MinimizeFile(args[0], output) }}
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output file (defaults to overwriting the input)")
	return cmd
}

func expandCmd() *cobra.Command {
	var output string
	cmd := &cobra.Command{Use: "expand <documax-file>", Short: "Decode GZ+B64 payloads into readable source", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error { return core.ExpandFile(args[0], output) }}
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output file (defaults to overwriting the input)")
	return cmd
}

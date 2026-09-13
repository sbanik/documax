package main

import (
	"os"

	"documax/internal/devtools"
)

func main() {
	if err := devtools.NewRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

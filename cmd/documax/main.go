package main

import (
	"os"

	"documax/internal/documax"
)

func main() {
	if err := documax.NewRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

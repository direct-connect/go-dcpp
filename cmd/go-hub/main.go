package main

import (
	"os"

	"github.com/direct-connect/go-dcpp/cmd/go-hub/cmd"
)

func main() {
	if err := cmd.Root.Execute(); err != nil {
		os.Exit(1)
	}
}

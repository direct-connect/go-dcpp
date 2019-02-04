package main

import (
	"fmt"
	"os"

	"github.com/direct-connect/go-dcpp/cmd/go-hub/cmd"
)

func main() {
	if err := cmd.Root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

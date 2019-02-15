package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"runtime"

	"github.com/spf13/cobra"

	dc "github.com/direct-connect/go-dcpp"
	"github.com/direct-connect/go-dcpp/version"
)

const Version = version.Vers

func main() {
	if err := Root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var Root = &cobra.Command{
	Use: "dcping <command>",
}

func init() {
	versionCmd := &cobra.Command{
		Use: "version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("Version:\t%s\nGo runtime:\t%s\n",
				Version, runtime.Version(),
			)
		},
	}
	Root.AddCommand(versionCmd)

	probeCmd := &cobra.Command{
		Use:   "probe <host:port> [...]",
		Short: "detects DC protocol used by the host",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("expected at least one address")
			}
			ctx := context.Background()
			for _, addr := range args {
				u, err := dc.Probe(ctx, addr)
				if err != nil {
					log.Println(err)
					fmt.Printf("%s - error\n", addr)
					continue
				}
				fmt.Printf("%s\n", u)
			}
			return nil
		},
	}
	Root.AddCommand(probeCmd)
}

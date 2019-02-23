package main

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"runtime"

	"github.com/direct-connect/go-dcpp/adc"
	"github.com/direct-connect/go-dcpp/nmdc"

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
		Use:   "probe host[:port] [...]",
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

	pingCmd := &cobra.Command{
		Use:   "ping [proto://]host[:port] [...]",
		Short: "pings the hub and returns its stats",
	}
	pingOut := pingCmd.Flags().String("out", "json", "output format (json or xml)")
	pingUsers := pingCmd.Flags().Bool("users", false, "return user list as well")
	pingDebug := pingCmd.Flags().Bool("debug", false, "print protocol messages to stderr")
	Root.AddCommand(pingCmd)
	pingCmd.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return errors.New("expected at least one address")
		}
		var (
			w   io.Writer = os.Stdout
			enc interface {
				Encode(interface{}) error
			}
		)
		switch *pingOut {
		case "json", "":
			enc = json.NewEncoder(w)
		case "xml":
			enc = xml.NewEncoder(w)
		default:
			return fmt.Errorf("unsupported format: %q", *pingOut)
		}
		nmdc.Debug = *pingDebug
		adc.Debug = *pingDebug

		ctx := context.Background()
		for _, addr := range args {
			info, err := dc.Ping(ctx, addr)
			if err != nil {
				log.Println(err)
				_ = enc.Encode(struct {
					Error string `json:"error"`
				}{Error: err.Error()})
				continue
			}
			if !*pingUsers {
				info.UserList = nil
			}
			if err = enc.Encode(info); err != nil {
				return err
			}
		}
		return nil
	}
}

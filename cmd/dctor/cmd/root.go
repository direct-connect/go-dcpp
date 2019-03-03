package cmd

import (
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/cretz/bine/tor"
	"github.com/direct-connect/go-dcpp/adc"
	ltor "github.com/ipsn/go-libtor"
)

const torSuffix = ".onion"

var Tor *tor.Tor

var Root = &cobra.Command{
	Use:   "dctor <command>",
	Short: "Tor proxy for DC network",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Starting Tor node...")
		var dw io.Writer
		if debug, _ := cmd.Flags().GetBool("debug"); debug {
			dw = os.Stderr
		}
		dataDir, _ := cmd.Flags().GetString("data")
		t, err := tor.Start(nil, &tor.StartConf{
			DataDir:        dataDir,
			ProcessCreator: ltor.Creator,
			DebugWriter:    dw,
		})
		if err != nil {
			return fmt.Errorf("failed to start Tor: %v", err)
		}
		Tor = t
		return nil
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		if Tor != nil {
			_ = Tor.Close()
		}
	},
}

func init() {
	Root.PersistentFlags().Bool("debug", false, "enable debug mode")
	Root.PersistentFlags().String("data", "tor-data", "tor data dir")
}

func proxyAll(c1, c2 *adc.Conn) error {
	errc := make(chan error, 2)
	go func() {
		errc <- proxyTo(c1, c2)
	}()
	go func() {
		errc <- proxyTo(c2, c1)
	}()
	return <-errc
}

func proxyTo(dst, src *adc.Conn) error {
	for {
		p, err := src.ReadPacket(time.Time{})
		if err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}
		err = dst.WritePacket(p)
		if err != nil {
			return err
		}
		err = dst.Flush()
		if err != nil {
			return err
		}
	}
}

func copyAll(c1, c2 net.Conn) error {
	errc := make(chan error, 2)
	go func() {
		_, err := io.Copy(c1, c2)
		errc <- err
	}()
	go func() {
		_, err := io.Copy(c2, c1)
		errc <- err
	}()
	return <-errc
}

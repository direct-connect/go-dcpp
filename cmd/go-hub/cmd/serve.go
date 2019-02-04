package cmd

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/direct-connect/go-dcpp/adc"
	"github.com/direct-connect/go-dcpp/hub"
	"github.com/direct-connect/go-dcpp/nmdc"
	"github.com/direct-connect/go-dcpp/version"
)

const Version = version.Vers

var Root = &cobra.Command{
	Use: "go-hub <command>",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		fmt.Printf("Hub version: %s\nGo runtime: %s\n\n",
			Version, runtime.Version(),
		)
	},
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "run the hub",
}

func init() {
	fDebug := serveCmd.Flags().Bool("debug", false, "print protocol logs to stderr")
	fPProf := serveCmd.Flags().Bool("pprof", false, "enable profiler endpoint")
	fName := serveCmd.Flags().String("name", "GoHub", "name of the hub")
	fDesc := serveCmd.Flags().String("desc", "Hybrid hub", "description of the hub")
	fSign := serveCmd.Flags().String("sign", "127.0.0.1", "host or IP to sign TLS certs for")
	fHost := serveCmd.Flags().String("host", ":1411", "host to listen on")
	Root.AddCommand(serveCmd)
	serveCmd.RunE = func(cmd *cobra.Command, args []string) error {
		cert, kp, err := loadCert(*fSign)
		if err != nil {
			return err
		}

		conf := &tls.Config{
			Certificates: []tls.Certificate{*cert},
		}
		_, port, _ := net.SplitHostPort(*fHost)
		addr := *fSign + ":" + port
		fmt.Println("listening on", *fHost)

		if *fDebug {
			fmt.Println("WARNING: protocol debug enabled")
			nmdc.Debug = true
			adc.Debug = true
		}

		if *fPProf {
			const pprofPort = ":6060"
			fmt.Println("enabling profiler on", pprofPort)
			go func() {
				if err := http.ListenAndServe(pprofPort, nil); err != nil {
					log.Println("cannot enable profiler:", err)
				}
			}()
		}

		h := hub.NewHub(hub.Info{
			Name: *fName,
			Desc: *fDesc,
			Addr: addr,
		}, conf)

		fmt.Printf(`

[ Hub URIs ]
adcs://%s?kp=%s
adcs://%s
adc://%s
dchub://%s

[ IRC chat ]
ircs://%s/hub
irc://%s/hub

[ HTTPS stats ]
https://%s%s

`,
			addr, kp,
			addr,
			addr,
			addr,

			addr,
			addr,

			addr, hub.HTTPInfoPathV0,
		)
		return h.ListenAndServe(*fHost)
	}
}

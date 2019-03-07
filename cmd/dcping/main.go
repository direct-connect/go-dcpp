package main

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	dc "github.com/direct-connect/go-dcpp"
	"github.com/direct-connect/go-dcpp/adc"
	"github.com/direct-connect/go-dcpp/hublist"
	"github.com/direct-connect/go-dcpp/nmdc"
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

type timeoutErr interface {
	Timeout() bool
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

	addrsCmd := &cobra.Command{
		Use:   "addrs hublist.xml.bz",
		Short: "read hub addresses from the file and prints them",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errors.New("expected file name")
			}
			f, err := os.Open(args[0])
			if err != nil {
				return err
			}
			defer f.Close()

			list, err := hublist.DecodeBZip2(f)
			if err != nil {
				return err
			}
			log.Println(len(list), "hubs")
			for _, h := range list {
				fmt.Print(h.Address + " ")
			}
			fmt.Println()
			return nil
		},
	}
	Root.AddCommand(addrsCmd)

	probeCmd := &cobra.Command{
		Use:   "probe host[:port] [...]",
		Short: "detects DC protocol used by the host",
	}
	probeTimeout := probeCmd.Flags().DurationP("timeout", "t", time.Second*5, "probe timeout")
	probeCmd.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return errors.New("expected at least one address")
		}
		rctx := context.Background()

		probeOne := func(addr string) {
			ctx, cancel := context.WithTimeout(rctx, *probeTimeout)
			defer cancel()

			u, err := dc.Probe(ctx, addr)
			if err != nil {
				log.Println(err)
				fmt.Printf("%s - error\n", addr)
			} else {
				fmt.Printf("%s\n", u)
			}
		}

		for _, addr := range args {
			probeOne(addr)
		}
		return nil
	}
	Root.AddCommand(probeCmd)

	pingCmd := &cobra.Command{
		Use:   "ping [proto://]host[:port] [...]",
		Short: "pings the hub and returns its stats",
	}
	pingOut := pingCmd.Flags().String("out", "json", "output format (json or xml)")
	pingUsers := pingCmd.Flags().Bool("users", false, "return user list as well")
	pingDebug := pingCmd.Flags().Bool("debug", false, "print protocol messages to stderr")
	pingPretty := pingCmd.Flags().Bool("pretty", false, "pretty-print an output")
	pingTimeout := pingCmd.Flags().DurationP("timeout", "t", time.Second*5, "ping timeout")
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
			e := json.NewEncoder(w)
			if *pingPretty {
				e.SetIndent("", "\t")
			}
			enc = e
		case "xml":
			e := xml.NewEncoder(w)
			if *pingPretty {
				e.Indent("", "\t")
			}
			enc = e
		default:
			return fmt.Errorf("unsupported format: %q", *pingOut)
		}
		nmdc.Debug = *pingDebug
		adc.Debug = *pingDebug

		rctx := context.Background()

		pingOne := func(addr string) error {
			ctx, cancel := context.WithTimeout(rctx, *pingTimeout)
			defer cancel()

			info, err := dc.Ping(ctx, addr)
			if info != nil && !*pingUsers {
				info.UserList = nil
			}
			isOffline := false
			if te, ok := err.(timeoutErr); ok && te.Timeout() {
				isOffline = true
			} else if e, ok := err.(*net.OpError); ok && e.Op == "dial" {
				switch e := e.Err.(type) {
				case *net.DNSError:
					isOffline = true
				case *os.SyscallError:
					if e, ok := e.Err.(syscall.Errno); ok {
						// TODO: windows
						switch e {
						case 0x6f: // connection refused
							isOffline = true
						case 0x71: // no route to host
							isOffline = true
						}
					}
				}
			}
			switch *pingOut {
			case "json", "":
				status := ""
				if err != nil {
					status = "error"
					if isOffline {
						status = "offline"
					} else {
						log.Println(err)
					}
				}
				if info == nil {
					_ = enc.Encode(struct {
						Addr   []string `json:"addr"`
						Status string   `json:"status,omitempty"`
					}{
						Addr:   []string{addr},
						Status: status,
					})
					return nil
				}
				if err = enc.Encode(struct {
					dc.HubInfo
					Status string `json:"status,omitempty"`
				}{
					HubInfo: *info,
					Status:  status,
				}); err != nil {
					return err
				}
			case "xml":
				var out hublist.Hub
				status := "Online"
				if err != nil {
					status = "Error"
					if isOffline {
						status = "Offline"
					} else {
						log.Println(err)
					}
				}
				if info != nil {
					out = hublist.Hub{
						Name:        info.Name,
						Address:     info.Addr[0],
						Description: info.Desc,
						Email:       info.Email,
						Encoding:    info.Enc,
						Icon:        info.Icon,
						Website:     info.Website,
						Users:       info.Users,
						Shared:      hublist.Size(info.Share),
					}
					// output encoding in the legacy format
					if strings.HasPrefix(out.Encoding, "windows-") {
						out.Encoding = "cp" + strings.TrimPrefix(out.Encoding, "windows-")
					}
					out.Encoding = strings.ToUpper(out.Encoding)
					if info.Server != nil {
						out.Software = info.Server.Name
					}
					for _, addr2 := range info.Addr[1:] {
						if !strings.HasPrefix(addr, addr2) && !strings.HasPrefix(addr2, addr) {
							out.Failover = addr2
							break
						}
					}
				}
				if out.Address == "" {
					out.Address = addr
				}
				out.Status = status
				if err = enc.Encode(out); err != nil {
					return err
				}
			default:
				return fmt.Errorf("unsupported format: %q", *pingOut)
			}
			return nil
		}

		for _, addr := range args {
			if err := pingOne(addr); err != nil {
				return err
			}
		}
		return nil
	}
}

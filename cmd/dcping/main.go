package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/text/encoding/htmlindex"

	adcp "github.com/direct-connect/go-dc/adc"
	dc "github.com/direct-connect/go-dcpp"
	"github.com/direct-connect/go-dcpp/adc"
	"github.com/direct-connect/go-dcpp/hublist"
	"github.com/direct-connect/go-dcpp/nmdc"
	"github.com/direct-connect/go-dcpp/version"
)

const Version = version.Vers

func main() {
	if err := Root.Execute(); err != nil {
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
	probeDebug := probeCmd.Flags().Bool("debug", false, "print protocol messages to stderr")
	probeTimeout := probeCmd.Flags().DurationP("timeout", "t", time.Second*3, "probe timeout")
	probeCmd.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return errors.New("expected at least one address")
		}
		cmd.SilenceUsage = true

		rctx := context.Background()
		dc.Debug = *probeDebug

		probeOne := func(addr string) error {
			ctx, cancel := context.WithTimeout(rctx, *probeTimeout)
			defer cancel()

			u, err := dc.Probe(ctx, addr)
			if err != nil {
				log.Println(err)
				fmt.Printf("%s - error\n", addr)
				return err
			}
			fmt.Printf("%s\n", u)
			return nil
		}

		var last error
		for _, addr := range args {
			if err := probeOne(addr); err != nil {
				last = err
			}
		}
		return last
	}
	Root.AddCommand(probeCmd)

	pingCmd := &cobra.Command{
		Use:   "ping [proto://]host[:port] [...]",
		Short: "pings the hub and returns its stats",
	}
	pingOut := pingCmd.Flags().String("out", "json", "output format (json, xml or xml-line)")
	pingUsers := pingCmd.Flags().Bool("users", false, "return user list as well")
	pingDebug := pingCmd.Flags().Bool("debug", false, "print protocol messages to stderr")
	pingPretty := pingCmd.Flags().Bool("pretty", false, "pretty-print an output")
	pingNum := pingCmd.Flags().IntP("num", "n", runtime.NumCPU()*2, "number of parallel pings")
	pingTimeout := pingCmd.Flags().DurationP("timeout", "t", time.Second*5, "ping timeout")
	pingFallbackEnc := pingCmd.Flags().StringP("encoding", "e", "", "fallback encoding")
	pingName := pingCmd.Flags().String("name", "", "name of the pinger")
	pingShare := pingCmd.Flags().Uint64("share", 0, "declared share size (in bytes)")
	pingShareFiles := pingCmd.Flags().Int("files", 0, "declared share files")
	pingSlots := pingCmd.Flags().Int("slots", 0, "declared slots")
	pingHubs := pingCmd.Flags().Int("hubs", 0, "declared hub count")
	Root.AddCommand(pingCmd)
	pingCmd.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return errors.New("expected at least one address")
		}
		if name := *pingFallbackEnc; name != "" {
			enc, err := htmlindex.Get(name)
			if err != nil {
				return err
			}
			nmdc.DefaultFallbackEncoding = enc
		}
		cmd.SilenceUsage = true

		var (
			mu    sync.Mutex
			w     io.Writer = os.Stdout
			enc   func(interface{}) error
			flush func() error
		)
		switch *pingOut {
		case "json", "":
			e := json.NewEncoder(w)
			if *pingPretty {
				e.SetIndent("", "\t")
			}
			enc = e.Encode
		case "xml", "xml-line":
			e := hublist.NewXMLWriter(w)
			if *pingName != "" {
				e.SetHublistName(*pingName)
			}
			if *pingOut == "xml-line" {
				e.Headers(false)
			}
			enc = func(o interface{}) error {
				return e.WriteHub(o.(hublist.Hub))
			}
			flush = e.Close
		default:
			return fmt.Errorf("unsupported format: %q", *pingOut)
		}
		cenc := enc
		enc = func(o interface{}) error {
			mu.Lock()
			defer mu.Unlock()
			return cenc(o)
		}
		nmdc.Debug = *pingDebug
		adc.Debug = *pingDebug
		dc.Debug = *pingDebug

		rctx := context.Background()

		conf := &dc.PingConfig{
			Name:       *pingName,
			ShareSize:  *pingShare,
			ShareFiles: *pingShareFiles,
			Slots:      *pingSlots,
			Hubs:       *pingHubs,
		}

		pingOne := func(addr string) error {
			ctx, cancel := context.WithTimeout(rctx, *pingTimeout)
			defer cancel()

			info, err := dc.Ping(ctx, addr, conf)
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
			var errCode int
			if e, ok := err.(adcp.Error); ok {
				errCode = int(e.Sev)*100 + e.Code
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
					_ = enc(struct {
						Addr    []string `json:"addr"`
						Status  string   `json:"status,omitempty"`
						ErrCode int      `json:"errcode,omitempty"`
					}{
						Addr:    []string{addr},
						Status:  status,
						ErrCode: errCode,
					})
					return err
				}
				if err := enc(struct {
					dc.HubInfo
					Status  string `json:"status,omitempty"`
					ErrCode int    `json:"errcode,omitempty"`
				}{
					HubInfo: *info,
					Status:  status,
					ErrCode: errCode,
				}); err != nil {
					panic(err)
				}
				return err
			case "xml", "xml-line":
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
						ErrCode:     errCode,
						KeyPrints:   info.KeyPrints,
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
				if err := enc(out); err != nil {
					panic(err)
				}
				return err
			default:
				panic(fmt.Errorf("unsupported format: %q", *pingOut))
			}
		}

		var wg sync.WaitGroup
		jobs := make(chan string, *pingNum)
		errc := make(chan error, 1)
		for i := 0; i < *pingNum; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for addr := range jobs {
					if err := pingOne(addr); err != nil {
						select {
						case errc <- err:
						default:
						}
					}
				}
			}()
		}

		for _, addr := range args {
			jobs <- addr
		}
		close(jobs)
		wg.Wait()
		if flush != nil {
			if err := flush(); err != nil {
				return err
			}
		}
		select {
		case err := <-errc:
			return err
		default:
		}
		return nil
	}
}

package hub

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"sync"

	"github.com/dennwc/go-dcpp/adc"
	"github.com/dennwc/go-dcpp/nmdc/hub"
)

type Info struct {
	Name    string
	Version string
	Desc    string
}

func NewHub(info Info, tls *tls.Config) *Hub {
	if info.Version == "" {
		info.Version = "go-dcpp 0.1"
	}
	if tls != nil {
		tls.NextProtos = []string{"adc", "nmdc"}
	}
	return &Hub{
		tls: tls,
		nmdc: hub.NewHub(hub.Info{
			Name:  info.Name,
			Topic: info.Desc,
		}),
		adcInfo: adc.HubInfo{
			Name:    info.Name,
			Version: info.Version,
			Desc:    info.Desc,
		},
	}
}

type Hub struct {
	tls *tls.Config

	nmdc *hub.Hub

	lastSID uint32
	adcInfo adc.HubInfo

	adcPeers struct {
		sync.RWMutex
		bySID  map[adc.SID]*adcConn
		byCID  map[adc.CID]*adcConn
		byName map[string]*adcConn
	}
}

func (h *Hub) ListenAndServe(addr string) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer lis.Close()
	for {
		conn, err := lis.Accept()
		if err != nil {
			return err
		}
		go func() {
			if err := h.Serve(conn); err != nil {
				log.Printf("%s: %v", conn.RemoteAddr(), err)
			}
		}()
	}
}

type timeoutErr interface {
	Timeout() bool
}

// Serve automatically detects the protocol and start the hub-client handshake.
func (h *Hub) Serve(conn net.Conn) error {
	defer conn.Close()

	// peek few bytes to detect the protocol
	conn, buf, err := peekCoon(conn, 4)
	if err != nil {
		if te, ok := err.(timeoutErr); ok && te.Timeout() {
			// only NMDC protocol expects the server to speak first
			return h.ServeNMDC(conn)
		}
		return err
	}

	if h.tls != nil && len(buf) >= 2 && string(buf[:2]) == "\x16\x03" {
		// TLS 1.x handshake
		tconn := tls.Server(conn, h.tls)
		if err := tconn.Handshake(); err != nil {
			_ = tconn.Close()
			return err
		}
		defer tconn.Close()

		// protocol negotiated by ALPN
		proto := tconn.ConnectionState().NegotiatedProtocol
		if proto != "" {
			log.Printf("%s: ALPN negotiated %q", tconn.RemoteAddr(), proto)
		} else {
			log.Printf("%s: ALPN not supported, fallback to ADC", tconn.RemoteAddr())
		}
		switch proto {
		case "nmdc":
			return h.ServeNMDC(tconn)
		case "":
			// it seems like only ADC supports TLS, so use it if ALPN is not supported
			fallthrough
		case "adc":
			return h.ServeADC(tconn)
		default:
			return fmt.Errorf("unsupported protocol: %q", proto)
		}
	} else if string(buf) == "HSUP" {
		// ADC client-hub handshake
		return h.ServeADC(conn)
	}
	return fmt.Errorf("unknown protocol magic: %q", string(buf))
}

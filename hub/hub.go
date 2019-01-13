package hub

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"

	"github.com/dennwc/go-dcpp/adc"
	"github.com/dennwc/go-dcpp/adc/types"
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
	h := &Hub{
		info: info,
		tls:  tls,
	}
	h.peers.logging = make(map[string]struct{})
	h.peers.byName = make(map[string]Peer)
	h.peers.bySID = make(map[adc.SID]Peer)
	h.initADC()
	return h
}

type Hub struct {
	info Info
	tls  *tls.Config

	lastSID uint32

	peers struct {
		sync.RWMutex
		// logging map is used to temporary bind a username.
		// The name should be removed from this map as soon as a byName entry is added.
		logging map[string]struct{}

		// byName tracks peers by their name.
		byName map[string]Peer
		bySID  map[adc.SID]Peer

		// ADC-specific

		loggingCID map[adc.CID]struct{}
		byCID      map[adc.CID]*adcPeer
	}
}

func (h *Hub) nextSID() adc.SID {
	// TODO: reuse SIDs
	v := atomic.AddUint32(&h.lastSID, 1)
	return types.SIDFromInt(v)
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

func (h *Hub) Peers() []Peer {
	h.peers.RLock()
	defer h.peers.RUnlock()
	return h.listPeers()
}

func (h *Hub) listPeers() []Peer {
	list := make([]Peer, 0, len(h.peers.byName))
	for _, p := range h.peers.byName {
		list = append(list, p)
	}
	return list
}

func (h *Hub) bySID(sid adc.SID) Peer {
	h.peers.RLock()
	p := h.peers.bySID[sid]
	h.peers.RUnlock()
	return p
}

func (h *Hub) broadcastUserJoin(peer Peer, notify []Peer) {
	log.Println("connected:", peer.SID(), peer.RemoteAddr(), peer.Name())
	if notify == nil {
		notify = h.Peers()
	}
	for _, p := range notify {
		_ = p.PeersJoin([]Peer{peer})
	}
}

func (h *Hub) broadcastUserLeave(peer Peer, name string, notify []Peer) {
	log.Println("disconnected:", peer.SID(), peer.RemoteAddr(), name)
	if notify == nil {
		notify = h.Peers()
	}
	for _, p := range notify {
		_ = p.PeersLeave([]Peer{peer})
	}
}

func (h *Hub) broadcastChat(from Peer, text string, notify []Peer) {
	if notify == nil {
		notify = h.Peers()
	}
	for _, p := range notify {
		_ = p.ChatMsg(from, text)
	}
}

func (h *Hub) sendMOTD(peer Peer) error {
	return peer.HubChatMsg("Welcome!")
}

type Software struct {
	Name string
	Vers string
}

type Peer interface {
	SID() adc.SID
	Name() string
	RemoteAddr() net.Addr
	Software() Software

	Close() error

	PeersJoin(peers []Peer) error
	//PeersUpdate(peers []Peer) error
	PeersLeave(peers []Peer) error

	ChatMsg(from Peer, text string) error
	PrivateMsg(from Peer, text string) error
	HubChatMsg(text string) error
}

type BasePeer struct {
	hub *Hub

	addr net.Addr
	sid  adc.SID
}

func (p *BasePeer) SID() adc.SID {
	return p.sid
}

func (p *BasePeer) RemoteAddr() net.Addr {
	return p.addr
}

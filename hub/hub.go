package hub

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/http2"

	"github.com/dennwc/go-dcpp/adc"
	"github.com/dennwc/go-dcpp/adc/types"
	"github.com/dennwc/go-dcpp/version"
)

type Info struct {
	Name string
	Desc string
	Soft Software
}

func NewHub(info Info, tls *tls.Config) *Hub {
	if info.Soft == (Software{}) {
		info.Soft = Software{
			Name: version.Name,
			Vers: version.Vers,
		}
	}
	if tls != nil {
		tls.NextProtos = []string{"adc", "nmdc"}
	}
	h := &Hub{
		created: time.Now(),
		info:    info,
		tls:     tls,
	}
	h.peers.logging = make(map[string]struct{})
	h.peers.byName = make(map[string]Peer)
	h.peers.bySID = make(map[adc.SID]Peer)
	h.initADC()
	h.initHTTP()
	return h
}

type Hub struct {
	created time.Time
	info    Info
	tls     *tls.Config
	h2      *http2.Server
	h2conf  *http2.ServeConnOpts

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

type Stats struct {
	Name  string   `json:"name"`
	Desc  string   `json:"desc,omitempty"`
	Users int      `json:"users"`
	Enc   string   `json:"enc,omitempty"`
	Soft  Software `json:"soft"`
	// TODO: uptime
}

func (h *Hub) Stats() Stats {
	h.peers.RLock()
	users := len(h.peers.byName)
	h.peers.RUnlock()
	return Stats{
		Name:  h.info.Name,
		Desc:  h.info.Desc,
		Users: users,
		Enc:   "utf8",
		Soft:  h.info.Soft,
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

// serve automatically detects the protocol and start the hub-client handshake.
func (h *Hub) serve(conn net.Conn, allowTLS bool) error {
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

	if allowTLS && h.tls != nil && len(buf) >= 2 && string(buf[:2]) == "\x16\x03" {
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
			log.Printf("%s: ALPN not supported, fallback to auto", tconn.RemoteAddr())
		}
		switch proto {
		case "nmdc":
			return h.ServeNMDC(tconn)
		case "adc":
			return h.ServeADC(tconn)
		case "h2":
			return h.ServeHTTP2(tconn)
		case "":
			// ALPN is not supported
			return h.serve(tconn, false)
		default:
			return fmt.Errorf("unsupported protocol: %q", proto)
		}
	}
	switch string(buf) {
	case "HSUP":
		// ADC client-hub handshake
		return h.ServeADC(conn)
	case "NICK":
		// IRC handshake
		return h.ServeIRC(conn)
	}
	return fmt.Errorf("unknown protocol magic: %q", string(buf))
}

// Serve automatically detects the protocol and start the hub-client handshake.
func (h *Hub) Serve(conn net.Conn) error {
	return h.serve(conn, true)
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

func (h *Hub) byName(name string) Peer {
	h.peers.RLock()
	p := h.peers.byName[name]
	h.peers.RUnlock()
	return p
}

func (h *Hub) bySID(sid adc.SID) Peer {
	h.peers.RLock()
	p := h.peers.bySID[sid]
	h.peers.RUnlock()
	return p
}

func (h *Hub) broadcastUserJoin(peer Peer, notify []Peer) {
	log.Printf("%s: connected: %s %s", peer.RemoteAddr(), peer.SID(), peer.Name())
	if notify == nil {
		notify = h.Peers()
	}
	for _, p := range notify {
		_ = p.PeersJoin([]Peer{peer})
	}
}

func (h *Hub) broadcastUserLeave(peer Peer, name string, notify []Peer) {
	log.Printf("%s: disconnected: %s %s", peer.RemoteAddr(), peer.SID(), name)
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

func (h *Hub) privateChat(from, to Peer, text string) {
	_ = to.PrivateMsg(from, text)
}

func (h *Hub) sendMOTD(peer Peer) error {
	return peer.HubChatMsg("Welcome!")
}

func (h *Hub) leave(peer Peer, sid adc.SID, name string) {
	h.peers.Lock()
	delete(h.peers.byName, name)
	delete(h.peers.bySID, sid)
	notify := h.listPeers()
	h.peers.Unlock()

	h.broadcastUserLeave(peer, name, notify)
}

func (h *Hub) leaveCID(peer Peer, sid adc.SID, cid adc.CID, name string) {
	h.peers.Lock()
	delete(h.peers.byName, name)
	delete(h.peers.bySID, sid)
	delete(h.peers.byCID, cid)
	notify := h.listPeers()
	h.peers.Unlock()

	h.broadcastUserLeave(peer, name, notify)
}

type Software struct {
	Name string `json:"name"`
	Vers string `json:"vers"`
}

type User struct {
	Name  string
	App   Software
	Share uint64
	Email string
}

type Peer interface {
	SID() adc.SID
	Name() string
	RemoteAddr() net.Addr
	User() User

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

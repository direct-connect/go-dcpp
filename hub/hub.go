package hub

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/http2"

	"github.com/direct-connect/go-dcpp/adc"
	"github.com/direct-connect/go-dcpp/adc/types"
	"github.com/direct-connect/go-dcpp/version"
)

type Config struct {
	Name        string
	Desc        string
	Addr        string
	Owner       string
	Website     string
	Email       string
	Soft        Software
	MOTD        string
	ChatLog     int
	ChatLogJoin int
	TLS         *tls.Config
}

func NewHub(conf Config) *Hub {
	if conf.Soft.Name == "" {
		conf.Soft.Name = version.Name
	}
	if conf.Soft.Vers == "" {
		conf.Soft.Vers = version.Vers
	}
	if conf.ChatLog < 0 {
		conf.ChatLog = 0
	}
	if conf.TLS != nil {
		conf.TLS.NextProtos = []string{"adc", "nmdc"}
	}
	h := &Hub{
		created: time.Now(),
		conf:    conf,
		tls:     conf.TLS,
	}
	h.peers.reserved = make(map[string]struct{})
	h.peers.byName = make(map[string]Peer)
	h.peers.bySID = make(map[SID]Peer)
	h.rooms.byName = make(map[string]*Room)
	h.rooms.bySID = make(map[SID]*Room)
	h.rooms.peers = make(map[Peer][]*Room)
	h.globalChat = h.newRoom("")
	h.initADC()
	h.initHTTP()
	h.userDB = NewUserDatabase()
	h.initCommands()
	return h
}

type SID = adc.SID

type Hub struct {
	created time.Time
	conf    Config
	tls     *tls.Config
	h2      *http2.Server
	h2conf  *http2.ServeConnOpts

	userDB UserDatabase

	lastSID uint32

	peers struct {
		sync.RWMutex
		// reserved map is used to temporary bind a username.
		// The name should be removed from this map as soon as a byName entry is added.
		reserved map[string]struct{}

		// byName tracks peers by their name.
		byName map[string]Peer
		bySID  map[SID]Peer

		// ADC-specific

		loggingCID map[adc.CID]struct{}
		byCID      map[adc.CID]*adcPeer
	}

	cmds struct {
		names  map[string]struct{} // no aliases
		byName map[string]*Command
	}

	globalChat *Room

	rooms struct {
		sync.RWMutex
		byName map[string]*Room
		bySID  map[SID]*Room
		peers  map[Peer][]*Room
	}
}

func (h *Hub) SetDatabase(db Database) {
	h.userDB = db
}

type Command struct {
	Path    []string
	Name    string
	Aliases []string
	Short   string
	Long    string
	Func    func(h *Hub, p Peer, args string) error
	run     func(p Peer, args string)
}

type Stats struct {
	Name     string   `json:"name"`
	Desc     string   `json:"desc,omitempty"`
	Addr     []string `json:"addr,omitempty"`
	Icon     string   `json:"icon,omitempty"`
	Owner    string   `json:"owner,omitempty"`
	Website  string   `json:"website,omitempty"`
	Email    string   `json:"email,omitempty"`
	Users    int      `json:"users"`
	MaxUsers int      `json:"max-users,omitempty"`
	Share    uint64   `json:"share,omitempty"`     // MB
	MaxShare uint64   `json:"max-share,omitempty"` // MB
	Enc      string   `json:"encoding,omitempty"`
	Soft     Software `json:"soft"`
	Uptime   uint64   `json:"uptime,omitempty"`
}

func (st *Stats) DefaultAddr() string {
	if len(st.Addr) == 0 {
		return ""
	}
	return st.Addr[0]
}

func (h *Hub) Stats() Stats {
	h.peers.RLock()
	users := len(h.peers.byName)
	h.peers.RUnlock()
	st := Stats{
		Name:    h.conf.Name,
		Desc:    h.conf.Desc,
		Owner:   h.conf.Owner,
		Website: h.conf.Website,
		Email:   h.conf.Email,
		Users:   users,
		Enc:     "utf-8",
		Soft:    h.conf.Soft,
	}
	if h.conf.Addr != "" {
		st.Addr = append(st.Addr, h.conf.Addr)
	}
	return st
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
			if err := h.Serve(conn); err != nil && err != io.EOF {
				log.Printf("%s: %v", conn.RemoteAddr(), err)
			}
		}()
	}
}

type timeoutErr interface {
	Timeout() bool
}

const peekTimeout = 650 * time.Millisecond

// serve automatically detects the protocol and start the hub-client handshake.
func (h *Hub) serve(conn net.Conn, allowTLS bool) error {
	defer conn.Close()

	timeout := peekTimeout
	if !allowTLS {
		timeout = 2 * peekTimeout
	}

	// peek few bytes to detect the protocol
	conn, buf, err := peekCoon(conn, 4, timeout)
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
	peer.BroadcastJoin(notify)
}

func (h *Hub) broadcastUserLeave(peer Peer, notify []Peer) {
	log.Printf("%s: disconnected: %s %s", peer.RemoteAddr(), peer.SID(), peer.Name())
	if notify == nil {
		notify = h.Peers()
	}
	peer.BroadcastLeave(notify)
}

func (h *Hub) privateChat(from, to Peer, m Message) {
	m.Time = time.Now().UTC()
	_ = to.PrivateMsg(from, m)
}

func (h *Hub) sendMOTD(peer Peer) error {
	motd := h.conf.MOTD
	if motd == "" {
		motd = "Welcome!"
	}
	return peer.HubChatMsg(motd)
}

func (h *Hub) cmdOutput(peer Peer, out string) {
	if strings.Contains(out, "\n") {
		out = "\n" + out
	}
	_ = peer.HubChatMsg(out)
}

func (h *Hub) cmdOutputf(peer Peer, format string, args ...interface{}) {
	h.cmdOutput(peer, fmt.Sprintf(format, args...))
}

func (h *Hub) cmdOutputJSON(peer Peer, out interface{}) {
	data, _ := json.MarshalIndent(out, "", "  ")
	h.cmdOutput(peer, string(data))
}

func (h *Hub) registerCommand(cmd Command) {
	cmd.run = func(p Peer, args string) {
		err := cmd.Func(h, p, args)
		if err != nil {
			h.cmdOutput(p, "error: "+err.Error())
		}
	}
	h.cmds.names[cmd.Name] = struct{}{}
	h.cmds.byName[cmd.Name] = &cmd
	for _, name := range cmd.Aliases {
		h.cmds.byName[name] = &cmd
	}
}

func (h *Hub) command(peer Peer, cmd string, args string) {
	c, ok := h.cmds.byName[cmd]
	if !ok {
		h.cmdOutput(peer, "unsupported command: "+cmd)
		return
	}
	c.run(peer, args)
}

func (h *Hub) ListCommands() []*Command {
	names := make([]string, 0, len(h.cmds.names))
	for name := range h.cmds.names {
		names = append(names, name)
	}
	command := make([]*Command, 0, len(names))
	for _, name := range names {
		if c := h.cmds.byName[name]; len(c.Path) != 0 {
			command = append(command, c)
		}
	}
	sort.Slice(command, func(i, j int) bool {
		a, b := command[i], command[j]
		l := len(a.Path)
		if len(a.Path) > len(b.Path) {
			l = len(b.Path)
		}
		for n := 0; n <= l; n++ {
			if a.Path[n] != b.Path[n] {
				return a.Path[n] < b.Path[n]
			}
		}
		return len(a.Path) <= len(b.Path)
	})
	return command
}

func (h *Hub) leave(peer Peer, sid adc.SID, name string, notify []Peer) {
	h.peers.Lock()
	delete(h.peers.byName, name)
	delete(h.peers.bySID, sid)
	if notify == nil {
		notify = h.listPeers()
	}
	h.leaveRooms(peer)
	h.peers.Unlock()

	h.broadcastUserLeave(peer, notify)
}

func (h *Hub) leaveCID(peer Peer, sid adc.SID, cid adc.CID, name string) {
	h.peers.Lock()
	delete(h.peers.byName, name)
	delete(h.peers.bySID, sid)
	delete(h.peers.byCID, cid)
	notify := h.listPeers()
	h.leaveRooms(peer)
	h.peers.Unlock()

	h.broadcastUserLeave(peer, notify)
}

func (h *Hub) leaveRooms(peer Peer) {
	h.globalChat.Leave(peer)
	h.rooms.Lock()
	list := h.rooms.peers[peer]
	delete(h.rooms.peers, peer)
	h.rooms.Unlock()
	for _, r := range list {
		r.Leave(peer)
	}
}

func (h *Hub) connectReq(from, to Peer, addr, token string, secure bool) {
	_ = to.ConnectTo(from, addr, token, secure)
}

func (h *Hub) revConnectReq(from, to Peer, token string, secure bool) {
	_ = to.RevConnectTo(from, token, secure)
}

type Software struct {
	Name string `json:"name"`
	Vers string `json:"vers"`
}

type User struct {
	Name           string
	App            Software
	HubsNormal     int
	HubsRegistered int
	HubsOperator   int
	Slots          int
	Share          uint64
	Email          string
	IPv4           bool
	IPv6           bool
	TLS            bool
}

package hub

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/htmlindex"

	dc "github.com/direct-connect/go-dc"
	"github.com/direct-connect/go-dcpp/adc"
	"github.com/direct-connect/go-dcpp/adc/types"
	"github.com/direct-connect/go-dcpp/version"
)

type Config struct {
	Name             string
	Desc             string
	Addr             string
	Owner            string
	Website          string
	Email            string
	Keyprint         string
	Soft             dc.Software
	MOTD             string
	ChatLog          int
	ChatLogJoin      int
	FallbackEncoding string
	TLS              *tls.Config
}

func NewHub(conf Config) (*Hub, error) {
	if conf.Name == "" {
		conf.Name = "GoHub"
	}
	if conf.Soft.Name == "" {
		conf.Soft.Name = version.Name
	}
	if conf.Soft.Version == "" {
		conf.Soft.Version = version.Vers
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
	if conf.FallbackEncoding != "" {
		enc, err := htmlindex.Get(conf.FallbackEncoding)
		if err != nil {
			return nil, err
		}
		h.fallback = enc
	}
	h.peers.reserved = make(map[string]struct{})
	h.peers.byName = make(map[string]Peer)
	h.peers.bySID = make(map[SID]Peer)
	h.rooms.byName = make(map[string]*Room)
	h.rooms.bySID = make(map[SID]*Room)
	h.globalChat = h.newRoom("")
	h.initADC()
	if err := h.initHTTP(); err != nil {
		return nil, err
	}
	h.userDB = NewUserDatabase()
	h.initCommands()
	return h, nil
}

type SID = adc.SID

type Hub struct {
	created time.Time
	conf    Config
	tls     *tls.Config
	h1      *http.Server
	h2      *http2.Server
	h2conf  *http2.ServeConnOpts

	userDB UserDatabase

	lastSID uint32

	fallback encoding.Encoding

	peers struct {
		curList atomic.Value // []Peer

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
	}

	plugins struct {
		loaded []Plugin
	}
}

func (h *Hub) SetDatabase(db Database) {
	h.userDB = db
}

type Command struct {
	Menu    []string
	Name    string
	Aliases []string
	Short   string
	Long    string
	Func    func(p Peer, args string) error
	run     func(p Peer, args string)
}

type Stats struct {
	Name     string      `json:"name"`
	Desc     string      `json:"desc,omitempty"`
	Addr     []string    `json:"addr,omitempty"`
	Icon     string      `json:"icon,omitempty"`
	Owner    string      `json:"owner,omitempty"`
	Website  string      `json:"website,omitempty"`
	Email    string      `json:"email,omitempty"`
	Users    int         `json:"users"`
	MaxUsers int         `json:"max-users,omitempty"`
	Share    uint64      `json:"share,omitempty"`     // MB
	MaxShare uint64      `json:"max-share,omitempty"` // MB
	Enc      string      `json:"encoding,omitempty"`
	Soft     dc.Software `json:"soft"`
	Uptime   uint64      `json:"uptime,omitempty"`
	Keyprint string      `json:"-"`
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
		Name:     h.conf.Name,
		Desc:     h.conf.Desc,
		Owner:    h.conf.Owner,
		Website:  h.conf.Website,
		Email:    h.conf.Email,
		Icon:     "icon.png",
		Users:    users,
		Enc:      "utf-8",
		Soft:     h.conf.Soft,
		Keyprint: h.conf.Keyprint,
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

func (h *Hub) Start() error {
	return h.initPlugins()
}

func (h *Hub) Close() error {
	h.stopPlugins()
	return nil
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
			return h.serveNMDC(conn)
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
		}
		switch proto {
		case "nmdc":
			return h.serveNMDC(tconn)
		case "adc":
			return h.ServeADC(tconn)
		case "http/0.9", "http/1.0", "http/1.1":
			return h.ServeHTTP1(tconn)
		case "h2", "h2c":
			return h.ServeHTTP2(tconn)
		case "":
			log.Printf("%s: ALPN not supported, fallback to auto", tconn.RemoteAddr())
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
	case "HEAD", "GET ", "POST", "PUT ", "DELE", "OPTI":
		// HTTP1 request
		return h.ServeHTTP1(conn)
	}
	return fmt.Errorf("unknown protocol magic: %q", string(buf))
}

// Serve automatically detects the protocol and start the hub-client handshake.
func (h *Hub) Serve(conn net.Conn) error {
	return h.serve(conn, true)
}

func (h *Hub) Peers() []Peer {
	if l := h.cachedList(); l != nil {
		return l
	}
	h.peers.RLock()
	defer h.peers.RUnlock()
	return h.listPeers()
}

func (h *Hub) cachedList() []Peer {
	list, _ := h.peers.curList.Load().([]Peer)
	return list
}

func (h *Hub) cacheList(list []Peer) {
	h.peers.curList.Store(list)
}

func (h *Hub) invalidateList() {
	h.peers.curList.Store([]Peer(nil))
}

func (h *Hub) listPeers() []Peer {
	if l := h.cachedList(); l != nil {
		return l
	}
	list := make([]Peer, 0, len(h.peers.byName))
	for _, p := range h.peers.byName {
		list = append(list, p)
	}
	h.cacheList(list)
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

func (h *Hub) RegisterCommand(cmd Command) {
	cmd.run = func(p Peer, args string) {
		err := cmd.Func(p, args)
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

func (h *Hub) isCommand(peer Peer, text string) bool {
	if text == "" {
		return true // pretend that this is a command
	} else if text == "/fav" {
		return true // special case
	}
	switch text[0] {
	case '!', '+', '/':
	default:
		return false
	}
	sub := strings.SplitN(text, " ", 2)
	cmd := sub[0][1:]
	args := ""
	if len(sub) > 1 {
		args = sub[1]
	}
	h.command(peer, cmd, args)
	return true
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
		if c := h.cmds.byName[name]; len(c.Menu) != 0 {
			command = append(command, c)
		}
	}
	sort.Slice(command, func(i, j int) bool {
		a, b := command[i], command[j]
		l := len(a.Menu)
		if len(a.Menu) > len(b.Menu) {
			l = len(b.Menu)
		}
		for n := 0; n <= l; n++ {
			if a.Menu[n] != b.Menu[n] {
				return a.Menu[n] < b.Menu[n]
			}
		}
		return len(a.Menu) <= len(b.Menu)
	})
	return command
}

func (h *Hub) leave(peer Peer, sid adc.SID, name string, notify []Peer) {
	h.peers.Lock()
	delete(h.peers.byName, name)
	delete(h.peers.bySID, sid)
	h.invalidateList()
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
	h.invalidateList()
	notify := h.listPeers()
	h.leaveRooms(peer)
	h.peers.Unlock()

	h.broadcastUserLeave(peer, notify)
}

func (h *Hub) leaveRooms(peer Peer) {
	h.globalChat.Leave(peer)
	pb := peer.base()
	pb.rooms.Lock()
	defer pb.rooms.Unlock()
	for _, r := range pb.rooms.list {
		r.Leave(peer)
	}
	pb.rooms.list = nil
}

func (h *Hub) connectReq(from, to Peer, addr, token string, secure bool) {
	_ = to.ConnectTo(from, addr, token, secure)
}

func (h *Hub) revConnectReq(from, to Peer, token string, secure bool) {
	_ = to.RevConnectTo(from, token, secure)
}

type User struct {
	Name           string
	App            dc.Software
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

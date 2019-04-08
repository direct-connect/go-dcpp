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

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/htmlindex"

	dc "github.com/direct-connect/go-dc"
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

const shareDiv = 1024 * 1024

type Hub struct {
	created time.Time
	conf    Config
	tls     *tls.Config
	httpData

	userDB UserDatabase

	lastSID uint32

	fallback encoding.Encoding

	peers struct {
		curList atomic.Value // []Peer
		share   int64        // atomic, MB

		sync.RWMutex
		// reserved map is used to temporary bind a username.
		// The name should be removed from this map as soon as a byName entry is added.
		reserved map[string]struct{}

		// byName tracks peers by their name.
		byName map[string]Peer
		bySID  map[SID]Peer

		adcPeers // ADC specific
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
	events events
}

func (h *Hub) SetDatabase(db Database) {
	h.userDB = db
}

func (h *Hub) incShare(v uint64) {
	atomic.AddInt64(&h.peers.share, int64(v/shareDiv))
	cntShare.Add(float64(v))
}

func (h *Hub) decShare(v uint64) {
	atomic.AddInt64(&h.peers.share, -int64(v/shareDiv))
	cntShare.Add(-float64(v))
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
	Share    uint64      `json:"share"`               // MB
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
		Share:    uint64(atomic.LoadInt64(&h.peers.share)),
		Enc:      "utf-8",
		Soft:     h.conf.Soft,
		Keyprint: h.conf.Keyprint,
	}
	if h.conf.Addr != "" {
		st.Addr = append(st.Addr, h.conf.Addr)
	}
	return st
}

func (h *Hub) nextSID() SID {
	// TODO: reuse SIDs
	v := atomic.AddUint32(&h.lastSID, 1)
	return sidFromInt(v)
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
		cntConnAccepted.Add(1)
		cntConnOpen.Add(1)
		go func() {
			defer func() {
				cntConnOpen.Add(-1)
				_ = conn.Close()
			}()
			if err := h.Serve(conn); err != nil && err != io.EOF {
				cntConnError.Add(1)
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

const (
	peekTimeout  = 650 * time.Millisecond
	writeTimeout = 10 * time.Second
)

// serve automatically detects the protocol and start the hub-client handshake.
func (h *Hub) serve(conn net.Conn, allowTLS bool) error {
	timeout := peekTimeout
	if !allowTLS {
		timeout = 2 * peekTimeout
	}

	start := time.Now()
	// peek few bytes to detect the protocol
	conn, buf, err := peekCoon(conn, 4, timeout)
	if err != nil {
		if te, ok := err.(timeoutErr); ok && te.Timeout() {
			cntConnAuto.Add(1)
			if !allowTLS {
				cntConnNMDCS.Add(1)
			}
			// only NMDC protocol expects the server to speak first
			return h.serveNMDC(conn)
		}
		return err
	}

	if pt := time.Since(start).Seconds(); allowTLS {
		durConnPeek.Observe(pt)
	} else {
		durConnPeekTLS.Observe(pt)
	}

	if allowTLS && h.tls != nil && len(buf) >= 2 && string(buf[:2]) == "\x16\x03" {
		// TLS 1.x handshake
		tconn := tls.Server(conn, h.tls)
		if err := tconn.Handshake(); err != nil {
			_ = tconn.Close()
			return err
		}
		defer tconn.Close()

		cntConnTLS.Add(1)

		// protocol negotiated by ALPN
		proto := tconn.ConnectionState().NegotiatedProtocol
		if proto != "" {
			cntConnALPN.Add(1)
			log.Printf("%s: ALPN negotiated %q", tconn.RemoteAddr(), proto)
		}
		switch proto {
		case "nmdc":
			cntConnNMDCS.Add(1)
			cntConnAlpnNMDC.Add(1)
			return h.serveNMDC(tconn)
		case "adc":
			cntConnADCS.Add(1)
			cntConnAlpnADC.Add(1)
			return h.ServeADC(tconn)
		case "http/0.9", "http/1.0", "http/1.1":
			cntConnHTTPS.Add(1)
			cntConnAlpnHTTP.Add(1)
			return h.ServeHTTP1(tconn)
		case "h2", "h2c":
			cntConnHTTPS.Add(1)
			cntConnAlpnHTTP.Add(1)
			return h.ServeHTTP2(tconn)
		case "":
			log.Printf("%s: ALPN not supported, fallback to auto", tconn.RemoteAddr())
			return h.serve(tconn, false)
		default:
			return fmt.Errorf("unsupported protocol: %q", proto)
		}
	}
	cntConnAuto.Add(1)
	switch string(buf) {
	case "HSUP":
		// ADC client-hub handshake
		if !allowTLS {
			cntConnADCS.Add(1)
		}
		return h.ServeADC(conn)
	case "NICK":
		// IRC handshake
		if !allowTLS {
			cntConnIRCS.Add(1)
		}
		return h.ServeIRC(conn)
	case "HEAD", "GET ", "POST", "PUT ", "DELE", "OPTI":
		// HTTP1 request
		if !allowTLS {
			cntConnHTTPS.Add(1)
		}
		return h.ServeHTTP1(conn)
	}
	return fmt.Errorf("unknown protocol magic: %q", string(buf))
}

// Serve automatically detects the protocol and start the hub-client handshake.
func (h *Hub) Serve(conn net.Conn) error {
	if !h.callOnConnected(conn) {
		_ = conn.Close()
		return nil
	}
	defer h.callOnDisconnected(conn)
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

func (h *Hub) PeerByName(name string) Peer {
	h.peers.RLock()
	p := h.peers.byName[name]
	h.peers.RUnlock()
	return p
}

func (h *Hub) bySID(sid SID) Peer {
	h.peers.RLock()
	p := h.peers.bySID[sid]
	h.peers.RUnlock()
	return p
}

// nameAvailable checks if a name can be bound. Returns false if a name is already in use.
// The callback can be passed to be executed under peers read lock.
func (h *Hub) nameAvailable(name string, fnc func()) bool {
	h.peers.RLock()
	_, sameName1 := h.peers.reserved[name]
	_, sameName2 := h.peers.byName[name]
	if fnc != nil {
		fnc()
	}
	h.peers.RUnlock()
	return !sameName1 && !sameName2
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
	cntChatMsgPM.Add(1)
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

func (h *Hub) leave(peer Peer, sid SID, name string, notify []Peer) {
	h.peers.Lock()
	delete(h.peers.byName, name)
	delete(h.peers.bySID, sid)
	h.invalidateList()
	if notify == nil {
		notify = h.listPeers()
	}
	h.leaveRooms(peer)
	h.peers.Unlock()
	cntPeers.Add(-1)
	h.decShare(peer.User().Share)

	h.broadcastUserLeave(peer, notify)
}

func (h *Hub) leaveCID(peer Peer, sid SID, cid CID, name string) {
	h.peers.Lock()
	delete(h.peers.byName, name)
	delete(h.peers.bySID, sid)
	delete(h.peers.byCID, cid)
	h.invalidateList()
	notify := h.listPeers()
	h.leaveRooms(peer)
	h.peers.Unlock()
	cntPeers.Add(-1)
	h.decShare(peer.User().Share)

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

func (h *Hub) SendGlobalChat(text string) {
	for _, p := range h.Peers() {
		_ = p.HubChatMsg(text)
	}
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

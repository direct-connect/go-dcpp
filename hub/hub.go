package hub

import (
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
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
		closed:  make(chan struct{}),
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
	h.rooms.init()
	h.globalChat = h.newRoom("")
	h.initADC()
	if err := h.initHTTP(); err != nil {
		return nil, err
	}
	h.db = NewDatabase()
	h.initCommands()
	return h, nil
}

const shareDiv = 1024 * 1024

type Hub struct {
	created time.Time
	closed  chan struct{}

	conf Config
	tls  *tls.Config
	httpData

	db Database

	lastSID uint32

	fallback encoding.Encoding

	sampler sampler

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
	rooms      rooms
	plugins    plugins
	hooks      hooks
	ipFilter   ipFilter
	profiles   profiles
}

func (h *Hub) SetDatabase(db Database) {
	h.db = db
}

func (h *Hub) incShare(v uint64) {
	atomic.AddInt64(&h.peers.share, int64(v/shareDiv))
	cntShare.Add(float64(v))
}

func (h *Hub) decShare(v uint64) {
	atomic.AddInt64(&h.peers.share, -int64(v/shareDiv))
	cntShare.Add(-float64(v))
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
	var errorsN uint64
	done := make(chan struct{})
	defer close(done)
	const maxErrorsPerSec = 25
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-h.closed:
				_ = lis.Close()
				return
			case <-ticker.C:
				atomic.StoreUint64(&errorsN, 0)
			}
		}
	}()
	for {
		conn, err := lis.Accept()
		if err != nil {
			if isTooManyFDs(err) {
				continue // skip "too many open files" error
			}
			return err
		}
		remote := conn.RemoteAddr()
		if h.IsHardBlocked(remote) {
			_ = conn.Close()
			cntConnBlocked.Add(1)
			continue
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
				if isProtocolErr(err) {
					h.probableAttack(remote, err)
				}
				if n := atomic.AddUint64(&errorsN, 1); n < maxErrorsPerSec {
					log.Printf("%s: %v", remote, err)
				}
			}
		}()
	}
}

func (h *Hub) Start() error {
	if err := h.loadProfiles(); err != nil {
		return err
	}
	if err := h.initPlugins(); err != nil {
		return err
	}
	go h.ipFilter.run(h.closed)
	return nil
}

func (h *Hub) Close() error {
	select {
	case <-h.closed:
		return nil
	default:
		close(h.closed)
	}
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
func (h *Hub) serve(conn net.Conn, insecure bool) error {
	timeout := peekTimeout
	if !insecure {
		timeout = 2 * peekTimeout
	}

	start := time.Now()
	// peek few bytes to detect the protocol
	conn, buf, err := peekCoon(conn, 4, timeout)
	if err != nil {
		if te, ok := err.(timeoutErr); ok && te.Timeout() {
			cntConnAuto.Add(1)
			if !insecure {
				cntConnNMDCS.Add(1)
			}
			// only NMDC protocol expects the server to speak first
			return h.serveNMDC(conn)
		}
		return err
	}

	if pt := time.Since(start).Seconds(); insecure {
		durConnPeek.Observe(pt)
	} else {
		durConnPeekTLS.Observe(pt)
	}

	if insecure && h.tls != nil && len(buf) >= 2 && string(buf[:2]) == "\x16\x03" {
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
		if !insecure {
			cntConnADCS.Add(1)
		}
		return h.ServeADC(conn)
	case "NICK":
		// IRC handshake
		if !insecure {
			cntConnIRCS.Add(1)
		}
		return h.ServeIRC(conn)
	case "HEAD", "GET ", "POST", "PUT ", "DELE", "OPTI":
		// HTTP1 request
		if !insecure {
			cntConnHTTPS.Add(1)
		}
		return h.ServeHTTP1(conn)
	}
	return &ErrUnknownProtocol{Magic: buf[:], Secure: !insecure}
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

// reserveName bind a provided name or return false otherwise.
//
// A pair of callbacks can be passed to be executed under peers write lock.
//
// The first callback is executed when the name can be bound and can provide additional
// checks or bind any other identifier. If callback returns false the name won't be bound
// and the function will return false.
//
// The second callback is executed after the name is unbound.
func (h *Hub) reserveName(name string, bind func() bool, unbind func()) (func(), bool) {
	h.peers.Lock()
	_, sameName1 := h.peers.reserved[name]
	_, sameName2 := h.peers.byName[name]
	if sameName1 || sameName2 {
		h.peers.Unlock()
		return nil, false
	}
	if bind != nil && !bind() {
		h.peers.Unlock()
		return nil, false
	}
	h.peers.reserved[name] = struct{}{}
	h.peers.Unlock()
	return func() {
		h.peers.Lock()
		delete(h.peers.reserved, name)
		if unbind != nil {
			unbind()
		}
		h.peers.Unlock()
	}, true
}

// acceptPeer removes the temporary name binding and accepts the peer on the hub.
// reserveName must be called before calling this method.
// Callbacks will be executed under peers write lock. The first callback is called before
// adding the user to the list and the second is executed after it was added to the list.
func (h *Hub) acceptPeer(peer Peer, pre, post func()) {
	sid := peer.SID()
	u := peer.UserInfo()

	h.peers.Lock()
	defer h.peers.Unlock()
	// cleanup temporary bindings
	delete(h.peers.reserved, u.Name)

	if pre != nil {
		pre()
	}

	// add user to the hub
	h.peers.bySID[sid] = peer
	h.peers.byName[u.Name] = peer
	h.invalidateList()
	h.globalChat.Join(peer)
	cntPeers.Add(1)
	h.incShare(u.Share)

	if post != nil {
		post()
	}
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

func (h *Hub) leave(peer Peer, sid SID, notify []Peer) {
	h.peers.Lock()
	delete(h.peers.byName, peer.Name())
	delete(h.peers.bySID, sid)
	h.invalidateList()
	if notify == nil {
		notify = h.listPeers()
	}
	h.leaveRooms(peer)
	h.peers.Unlock()
	cntPeers.Add(-1)
	h.decShare(peer.UserInfo().Share)

	h.broadcastUserLeave(peer, notify)
}

func (h *Hub) leaveCID(peer Peer, sid SID, cid CID) {
	h.peers.Lock()
	delete(h.peers.byName, peer.Name())
	delete(h.peers.bySID, sid)
	delete(h.peers.byCID, cid)
	h.invalidateList()
	notify := h.listPeers()
	h.leaveRooms(peer)
	h.peers.Unlock()
	cntPeers.Add(-1)
	h.decShare(peer.UserInfo().Share)

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

type UserInfo struct {
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

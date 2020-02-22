package hub

import (
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/htmlindex"

	"github.com/direct-connect/go-dc/types"
	"github.com/direct-connect/go-dcpp/internal/safe"
	"github.com/direct-connect/go-dcpp/version"
)

type Config struct {
	Name             string
	Desc             string
	Topic            string
	Addr             string
	Owner            string
	Website          string
	Email            string
	Private          bool
	Keyprint         string
	Soft             types.Software
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
		conf.Soft.Name = version.HubName
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
	if conf.Desc == "" {
		conf.Desc = "Hybrid hub"
	}
	if conf.Topic == "" {
		conf.Topic = conf.Desc
	}
	if conf.MOTD == "" {
		conf.MOTD = "Welcome!"
	}
	h := &Hub{
		created: time.Now(),
		closed:  make(chan struct{}),
		tls:     conf.TLS,
	}
	h.conf.Config = conf
	h.conf.private = conf.Private
	h.setZlibLevel(-1)
	if conf.FallbackEncoding != "" {
		enc, err := htmlindex.Get(conf.FallbackEncoding)
		if err != nil {
			return nil, err
		}
		h.fallback = enc
	}
	h.peers.reserved = make(map[nameKey]struct{})
	h.peers.byName = make(map[nameKey]Peer)
	h.peers.bySID = make(map[SID]Peer)
	h.rooms.init()
	h.globalChat = h.newRoom("")

	var err error
	h.hubUser, err = h.newBot(conf.Name, conf.Desc, conf.Email, UserHub, conf.Soft)
	if err != nil {
		return nil, err
	}

	h.initADC()
	if err := h.initHTTP(); err != nil {
		return nil, err
	}
	h.db = NewDatabase()
	h.initCommands()
	return h, nil
}

const shareDiv = 1024 * 1024

// nameKey is a lowercase name.
type nameKey string

func toNameKey(name string) nameKey {
	return nameKey(strings.ToLower(name))
}

type Hub struct {
	created time.Time
	closed  chan struct{}

	conf struct {
		private bool

		sync.RWMutex
		Config
		m Map
	}
	addrs []string
	tls   *tls.Config
	httpData

	db Database

	lastSID uint32
	hubUser *Bot

	fallback encoding.Encoding

	sampler sampler

	peers struct {
		curList atomic.Value // []Peer
		share   int64        // atomic, MB

		sync.RWMutex
		// reserved map is used to temporary bind a username.
		// The name should be removed from this map as soon as a byName entry is added.
		reserved map[nameKey]struct{}

		// byName tracks peers by their name.
		byName map[nameKey]Peer
		bySID  map[SID]Peer

		adcPeers // ADC specific
	}

	cmds struct {
		names  map[string]struct{} // no aliases
		byName map[string]*Command
	}
	zlib struct {
		level int32
	}
	redirect struct {
		nmdcToTLS safe.Bool
		nmdcToADC safe.Bool
		adcToTLS  safe.Bool
	}

	globalChat *Room
	rooms      rooms
	plugins    plugins
	hooks      hooks
	bans       bans
	profiles   profiles
}

func (h *Hub) SetDatabase(db Database) {
	h.db = db
}

func (h *Hub) AddAddress(addr string) {
	h.addrs = append(h.addrs, addr)
}

func (h *Hub) IsPrivate() bool {
	return h.conf.private
}

func (h *Hub) incShare(v uint64) {
	atomic.AddInt64(&h.peers.share, int64(v/shareDiv))
	cntShare.Add(float64(v))
}

func (h *Hub) decShare(v uint64) {
	atomic.AddInt64(&h.peers.share, -int64(v/shareDiv))
	cntShare.Add(-float64(v))
}

func (h *Hub) zlibLevel() int {
	return int(atomic.LoadInt32(&h.zlib.level))
}

func (h *Hub) setZlibLevel(level int) {
	if level < -1 {
		level = -1
	}
	if level > 9 {
		level = 9
	}
	atomic.StoreInt32(&h.zlib.level, int32(level))
}

func (h *Hub) getRedirectNMDCToTLS() bool {
	return h.redirect.nmdcToTLS.Get()
}

func (h *Hub) getRedirectNMDCToADC() bool {
	return h.redirect.nmdcToADC.Get()
}

func (h *Hub) getRedirectADCToTLS() bool {
	return h.redirect.adcToTLS.Get()
}

func (h *Hub) setRedirectNMDCToTLS(v bool) {
	h.redirect.nmdcToTLS.Set(v)
}

func (h *Hub) setRedirectNMDCToADC(v bool) {
	h.redirect.nmdcToADC.Set(v)
}

func (h *Hub) setRedirectADCToTLS(v bool) {
	h.redirect.adcToTLS.Set(v)
}

type Stats struct {
	Name     string         `json:"name"`
	Desc     string         `json:"desc,omitempty"`
	Addr     []string       `json:"addr,omitempty"`
	Private  bool           `json:"private,omitempty"`
	Icon     string         `json:"icon,omitempty"`
	Owner    string         `json:"owner,omitempty"`
	Website  string         `json:"website,omitempty"`
	Email    string         `json:"email,omitempty"`
	Users    int            `json:"users"`
	MaxUsers int            `json:"max-users,omitempty"`
	Share    uint64         `json:"share"`               // MB
	MaxShare uint64         `json:"max-share,omitempty"` // MB
	Enc      string         `json:"encoding,omitempty"`
	Soft     types.Software `json:"soft"`
	Uptime   uint64         `json:"uptime,omitempty"`
	Keyprint string         `json:"-"`
}

func (st *Stats) DefaultAddr() string {
	if len(st.Addr) == 0 {
		return ""
	}
	return st.Addr[0]
}

func (h *Hub) Uptime() time.Duration {
	return time.Since(h.created)
}

func (h *Hub) Stats() Stats {
	h.peers.RLock()
	users := len(h.peers.byName)
	h.peers.RUnlock()
	h.conf.RLock()
	st := Stats{
		Name:     h.conf.Name,
		Desc:     h.conf.Desc,
		Owner:    h.conf.Owner,
		Website:  h.conf.Website,
		Email:    h.conf.Email,
		Private:  h.conf.Private,
		Icon:     "icon.png",
		Users:    users,
		Share:    uint64(atomic.LoadInt64(&h.peers.share)),
		Enc:      "utf-8",
		Soft:     h.conf.Soft,
		Keyprint: h.conf.Keyprint,
		Uptime:   uint64(h.Uptime().Seconds()),
	}
	if h.conf.Addr != "" {
		st.Addr = append(st.Addr, h.conf.Addr)
	}
	h.conf.RUnlock()
	st.Addr = append(st.Addr, h.addrs...)
	return st
}

func (h *Hub) nextSID() SID {
	// TODO: reuse SIDs
	v := atomic.AddUint32(&h.lastSID, 1)
	return sidFromInt(v)
}

func (h *Hub) HubUser() *Bot {
	return h.hubUser
}

func (h *Hub) getSoft() types.Software {
	return h.conf.Soft // won't change
}

func (h *Hub) getName() string {
	h.conf.RLock()
	name := h.conf.Name
	h.conf.RUnlock()
	return name
}

func (h *Hub) getTopic() string {
	h.conf.RLock()
	topic := h.conf.Topic
	if topic == "" {
		topic = h.conf.Desc
	}
	h.conf.RUnlock()
	return topic
}

func (h *Hub) getMOTD() string {
	h.conf.RLock()
	motd := h.conf.MOTD
	if motd == "" {
		motd = h.conf.Desc
	}
	if motd == "" {
		motd = h.conf.Topic
	}
	h.conf.RUnlock()
	return motd
}

func (h *Hub) setName(name string) {
	h.conf.Lock()
	h.conf.Name = name
	h.conf.Unlock()
	// TODO: rename the hub bot
}

func (h *Hub) setDesc(desc string) {
	h.conf.Lock()
	h.conf.Desc = desc
	h.conf.Unlock()
}

func (h *Hub) setTopic(topic string) {
	h.conf.Lock()
	h.conf.Topic = topic
	h.conf.Unlock()
	h.broadcastTopic(topic)
}

func (h *Hub) setMOTD(motd string) {
	h.conf.Lock()
	h.conf.MOTD = motd
	h.conf.Unlock()
}

func (h *Hub) poweredBy() string {
	soft := h.getSoft()
	uptime := h.Uptime().String()
	if i := strings.LastIndexByte(uptime, '.'); i > 0 {
		uptime = uptime[:i] + "s"
	}
	return strings.Join([]string{"Powered by", soft.Name, soft.Version, "(uptime:", uptime + ")"}, " ")
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
	if err := h.loadBans(); err != nil {
		return err
	}
	if err := h.initPlugins(); err != nil {
		return err
	}
	go h.bans.run(h.closed)
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
func (h *Hub) serve(conn net.Conn, cinfo *ConnInfo) error {
	timeout := peekTimeout
	if cinfo.Secure {
		timeout = 2 * peekTimeout
	}

	start := time.Now()
	// peek few bytes to detect the protocol
	conn, buf, err := peekCoon(conn, 4, timeout)
	if err != nil {
		if te, ok := err.(timeoutErr); ok && te.Timeout() {
			cntConnAuto.Add(1)
			// only NMDC protocol expects the server to speak first
			return h.ServeNMDC(conn, cinfo)
		}
		return err
	}

	if pt := time.Since(start).Seconds(); cinfo.TLSVers != 0 {
		durConnPeek.Observe(pt)
	} else {
		durConnPeekTLS.Observe(pt)
	}

	if cinfo.TLSVers == 0 && h.tls != nil && len(buf) >= 2 && string(buf[:2]) == "\x16\x03" {
		// TLS 1.x handshake
		tconn := tls.Server(conn, h.tls)
		if err := tconn.Handshake(); err != nil {
			_ = tconn.Close()
			return err
		}
		defer tconn.Close()

		cntConnTLS.Add(1)

		st := tconn.ConnectionState()
		cinfo.Secure = true
		cinfo.TLSVers = st.Version

		// protocol negotiated by ALPN
		proto := st.NegotiatedProtocol
		if proto != "" {
			cntConnALPN.Add(1)
			cinfo.ALPN = proto
			log.Printf("%s: ALPN negotiated %q", tconn.RemoteAddr(), proto)
		}
		switch proto {
		case "nmdc":
			return h.ServeNMDC(tconn, cinfo)
		case "adc":
			return h.ServeADC(tconn, cinfo)
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
			return h.serve(tconn, cinfo)
		default:
			return fmt.Errorf("unsupported protocol: %q", proto)
		}
	}
	cntConnAuto.Add(1)
	switch string(buf) {
	case "HSUP":
		// ADC client-hub handshake
		return h.ServeADC(conn, cinfo)
	case "NICK":
		// IRC handshake
		return h.ServeIRC(conn, cinfo)
	case "HEAD", "GET ", "POST", "PUT ", "DELE", "OPTI":
		// HTTP1 request
		if cinfo.Secure {
			cntConnHTTPS.Add(1)
		}
		return h.ServeHTTP1(conn)
	}
	return &ErrUnknownProtocol{Magic: buf[:], Secure: cinfo.Secure}
}

// Serve automatically detects the protocol and start the hub-client handshake.
func (h *Hub) Serve(conn net.Conn) error {
	if !h.callOnConnected(conn) {
		cntConnBlocked.Add(1)
		_ = conn.Close()
		return nil
	}
	defer h.callOnDisconnected(conn)
	return h.serve(conn, &ConnInfo{
		Local:  conn.LocalAddr(),
		Remote: conn.RemoteAddr(),
	})
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
	key := toNameKey(name)
	h.peers.RLock()
	p := h.peers.byName[key]
	h.peers.RUnlock()
	return p
}

func (h *Hub) peerBySID(sid SID) Peer {
	h.peers.RLock()
	p := h.peers.bySID[sid]
	h.peers.RUnlock()
	return p
}

// nameAvailable checks if a name can be bound. Returns false if a name is already in use.
// The callback can be passed to be executed under peers read lock.
func (h *Hub) nameAvailable(name string, fnc func()) bool {
	key := toNameKey(name)
	h.peers.RLock()
	_, sameName1 := h.peers.reserved[key]
	_, sameName2 := h.peers.byName[key]
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
	key := toNameKey(name)
	h.peers.Lock()
	_, sameName1 := h.peers.reserved[key]
	_, sameName2 := h.peers.byName[key]
	if sameName1 || sameName2 {
		h.peers.Unlock()
		return nil, false
	}
	if bind != nil && !bind() {
		h.peers.Unlock()
		return nil, false
	}
	h.peers.reserved[key] = struct{}{}
	h.peers.Unlock()
	return func() {
		h.peers.Lock()
		delete(h.peers.reserved, key)
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

	key := toNameKey(u.Name)
	h.peers.Lock()
	defer h.peers.Unlock()
	// cleanup temporary bindings
	delete(h.peers.reserved, key)

	if pre != nil {
		pre()
	}

	// add user to the hub
	h.peers.bySID[sid] = peer
	h.peers.byName[key] = peer
	h.invalidateList()
	h.globalChat.Join(peer)
	cntPeers.Add(1)
	h.incShare(u.Share)

	if post != nil {
		post()
	}
}

func topicMsg(topic string) Message {
	return Message{Text: "topic: " + topic, Me: true}
}

func (h *Hub) broadcastTopic(topic string) {
	for _, p2 := range h.Peers() {
		if pt, ok := p2.(PeerTopic); ok {
			_ = pt.Topic(topic)
		} else {
			_ = p2.HubChatMsg(topicMsg(topic))
		}
	}
}

func (h *Hub) broadcastUserJoin(peer Peer, notify []Peer) {
	log.Printf("%s: connected: %s %s", peer.RemoteAddr(), peer.SID(), peer.Name())
	if notify == nil {
		notify = h.Peers()
	}
	e := &PeersJoinEvent{Peers: []Peer{peer}}
	for _, p2 := range notify {
		_ = p2.PeersJoin(e)
	}
}

func (h *Hub) broadcastUserUpdate(peer Peer, notify []Peer) {
	if notify == nil {
		notify = h.Peers()
	}
	e := &PeersUpdateEvent{Peers: []Peer{peer}}
	for _, p2 := range notify {
		_ = p2.PeersUpdate(e)
	}
}

func (h *Hub) broadcastUserLeave(peer Peer, notify []Peer) {
	log.Printf("%s: disconnected: %s %s", peer.RemoteAddr(), peer.SID(), peer.Name())
	if notify == nil {
		notify = h.Peers()
	}
	e := &PeersLeaveEvent{Peers: []Peer{peer}}
	for _, p2 := range notify {
		_ = p2.PeersLeave(e)
	}
}

func (h *Hub) privateChat(from, to Peer, m Message) {
	if !h.callOnPM(from, to, m) {
		cntChatMsgPMDropped.Add(1)
		return
	}
	cntChatMsgPM.Add(1)
	m.Time = time.Now().UTC()
	_ = to.PrivateMsg(from, m)
}

func (h *Hub) sendMOTD(peer Peer) error {
	return peer.HubChatMsg(Message{Text: h.getMOTD()})
}

func (h *Hub) leave(peer Peer, sid SID, notify []Peer) {
	key := toNameKey(peer.Name())
	h.peers.Lock()
	delete(h.peers.byName, key)
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
	key := toNameKey(peer.Name())
	h.peers.Lock()
	delete(h.peers.byName, key)
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
	m := Message{Text: text}
	for _, p := range h.Peers() {
		_ = p.HubChatMsg(m)
	}
}

type UserKind int

const (
	UserNormal = UserKind(iota)
	UserHub
	UserBot
)

type UserInfo struct {
	Name           string
	Desc           string
	Kind           UserKind
	App            types.Software
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

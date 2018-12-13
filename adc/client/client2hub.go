package client

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"strconv"
	"sync"
	"time"

	"github.com/dennwc/go-dcpp/adc"
)

// DialHub connects to a hub and runs a handshake.
func DialHub(addr string, info *Config) (*Conn, error) {
	conn, err := adc.Dial(addr)
	if err != nil {
		return nil, err
	}
	return HubHandshake(conn, info)
}

type Config struct {
	PID        adc.PID
	Name       string
	Extensions adc.ExtFeatures
}

func (c *Config) validate() error {
	if c.PID.IsZero() {
		return errors.New("PID should not be empty")
	}
	if c.Name == "" {
		return errors.New("name should be set")
	}
	return nil
}

// HubHandshake begins a Client-Hub handshake on a connection.
func HubHandshake(conn *adc.Conn, conf *Config) (*Conn, error) {
	if err := conf.validate(); err != nil {
		return nil, err
	}
	sid, mutual, err := protocolToHub(conn)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	c := &Conn{
		conn: conn,
		sid:  sid, pid: conf.PID,
		fea:     mutual,
		closing: make(chan struct{}),
		closed:  make(chan struct{}),
	}
	c.user.Pid = &conf.PID
	c.user.Name = conf.Name
	c.user.Features = conf.Extensions

	if err := identifyToHub(conn, sid, &c.user); err != nil {
		conn.Close()
		return nil, err
	}
	c.ext = make(map[adc.Feature]struct{})
	for _, ext := range c.user.Features {
		c.ext[ext] = struct{}{}
	}
	if err := c.acceptUsersList(); err != nil {
		conn.Close()
		return nil, err
	}
	c.conn.KeepAlive(time.Minute / 2)
	go c.readLoop()
	return c, nil
}

func protocolToHub(conn *adc.Conn) (adc.SID, adc.ModFeatures, error) {
	ourFeatures := adc.ModFeatures{
		// should always be set for ADC
		adc.FeaBASE: true,
		adc.FeaTIGR: true,
		// extensions
		adc.FeaPING: true,
		adc.FeaBZIP: true,
		// TODO: ZLIG
	}

	// Send supported features (SUP), initiating the PROTOCOL state.
	// We expect SUP followed by SID to transition to IDENTIFY.
	//
	// https://adc.sourceforge.io/ADC.html#_protocol
	err := conn.WriteHubMsg(adc.Supported{
		Features: ourFeatures,
	})
	if err != nil {
		return adc.SID{}, nil, err
	}
	if err := conn.Flush(); err != nil {
		return adc.SID{}, nil, err
	}
	// shouldn't take longer than this
	deadline := time.Now().Add(time.Second * 5)

	// first, we expect a SUP from the hub with a list of supported features
	msg, err := conn.ReadInfoMsg(deadline)
	if err != nil {
		return adc.SID{}, nil, err
	}
	sup, ok := msg.(adc.Supported)
	if !ok {
		return adc.SID{}, nil, fmt.Errorf("expected SUP command, got: %#v", msg)
	}
	hubFeatures := sup.Features

	// check mutual features
	mutual := ourFeatures.Intersect(hubFeatures)
	if !mutual.IsSet(adc.FeaBASE) && mutual.IsSet(adc.FeaBAS0) {
		return adc.SID{}, nil, fmt.Errorf("hub does not support BASE")
	} else if !mutual.IsSet(adc.FeaTIGR) {
		return adc.SID{}, nil, fmt.Errorf("hub does not support TIGR")
	}

	// next, we expect a SID that will assign a Session ID
	msg, err = conn.ReadInfoMsg(deadline)
	if err != nil {
		return adc.SID{}, nil, err
	}
	sid, ok := msg.(adc.SIDAssign)
	if !ok {
		return adc.SID{}, nil, fmt.Errorf("expected SID command, got: %#v", msg)
	}
	return sid.SID, mutual, nil
}

func identifyToHub(conn *adc.Conn, sid adc.SID, user *adc.User) error {
	// Hub may send INF, but it's not required.
	// The client should broadcast INF with PD/ID and other required fields.
	//
	// https://adc.sourceforge.io/ADC.html#_identify

	if user.Id.IsZero() {
		user.Id = user.Pid.Hash()
	}
	if user.Application == "" {
		user.Application = "go-dcpp"
		user.Version = "0.1"
	}
	for _, f := range []adc.Feature{adc.FeaSEGA, adc.FeaTCP4} {
		if !user.Features.Has(f) {
			user.Features = append(user.Features, f)
		}
	}
	err := conn.WriteBroadcast(sid, user)
	if err != nil {
		return err
	}
	if err := conn.Flush(); err != nil {
		return err
	}
	// TODO: registered user
	return nil
}

// Conn represents a Client-to-Hub connection.
type Conn struct {
	conn *adc.Conn
	fea  adc.ModFeatures

	closing chan struct{}
	closed  chan struct{}

	pid  adc.PID
	sid  adc.SID
	user adc.User
	ext  map[adc.Feature]struct{}

	hub adc.HubInfo

	peers struct {
		sync.RWMutex
		// keeps both online and offline users
		byCID map[adc.CID]*Peer
		// only keeps online users
		bySID map[adc.SID]*Peer
	}

	revConn struct {
		sync.Mutex
		tokens map[string]revConnToken
	}
}

type revConnToken struct {
	cid    adc.CID
	cancel <-chan struct{}
	addr   chan string
	errc   chan error
}

// PID returns Private ID associated with this connection.
func (c *Conn) PID() adc.PID { return c.pid }

// CID returns Client ID associated with this connection.
func (c *Conn) CID() adc.CID { return c.user.Id }

// SID returns Session ID associated with this connection.
// Only valid after a Client-Hub handshake.
func (c *Conn) SID() adc.SID { return c.sid }

// Hub returns hub information.
func (c *Conn) Hub() adc.HubInfo { return c.hub }

// Features returns a set of negotiated features.
func (c *Conn) Features() adc.ModFeatures { return c.fea.Clone() }

func (c *Conn) Close() error {
	select {
	case <-c.closing:
		<-c.closed
		return nil
	default:
	}
	close(c.closing)
	err := c.conn.Close()
	<-c.closed
	return err
}

func (c *Conn) writeDirect(to adc.SID, msg adc.Message) error {
	if err := c.conn.WriteDirect(c.SID(), to, msg); err != nil {
		return err
	}
	return c.conn.Flush()
}

func (c *Conn) revConnToken(ctx context.Context, cid adc.CID) (token string, addr <-chan string, _ <-chan error) {
	ch := make(chan string, 1)
	errc := make(chan error, 1)
	for {
		tok := strconv.Itoa(rand.Int())
		c.revConn.Lock()
		_, ok := c.revConn.tokens[tok]
		if !ok {
			if c.revConn.tokens == nil {
				c.revConn.tokens = make(map[string]revConnToken)
			}
			c.revConn.tokens[tok] = revConnToken{cancel: ctx.Done(), cid: cid, addr: ch, errc: errc}
			c.revConn.Unlock()
			return tok, ch, errc
		}
		c.revConn.Unlock()
		// collision, pick another token
	}
}

func (c *Conn) readLoop() {
	defer close(c.closed)
	for {
		cmd, err := c.conn.ReadPacket(time.Time{})
		if err != nil {
			log.Println(err)
			return
		}
		switch cmd := cmd.(type) {
		case *adc.BroadcastPacket:
			if err := c.handleBroadcast(cmd); err != nil {
				log.Println(err)
				return
			}
		case *adc.InfoPacket:
			if err := c.handleInfo(cmd); err != nil {
				log.Println(err)
				return
			}
		case *adc.FeaturePacket:
			if err := c.handleFeature(cmd); err != nil {
				log.Println(err)
				return
			}
		case *adc.DirectPacket:
			// TODO: ADC flaw: why ever send the client his own SID? hub should append it instead
			//		 same for the sending party
			if cmd.Targ != c.SID() {
				log.Println("direct command to a wrong destination:", cmd.Targ)
				return
			}
			if err := c.handleDirect(cmd); err != nil {
				log.Println(err)
				return
			}
		default:
			log.Printf("unhandled command: %T", cmd)
		}
	}
}

func (c *Conn) handleBroadcast(p *adc.BroadcastPacket) error {
	// we could decode the message and type-switch, but for cases
	// below it's better to decode later
	switch p.Name {
	case (adc.User{}).Cmd():
		// easier to merge while decoding
		return c.peerUpdate(p.ID, p.Data)
	case (adc.SearchRequest{}).Cmd():
		peer := c.peerBySID(p.ID)
		// async decoding
		go c.handleSearch(peer, p.Data)
		return nil
	// TODO: MSG
	default:
		log.Printf("unhandled broadcast command: %v", p.Name)
		return nil
	}
}

func (c *Conn) handleInfo(p *adc.InfoPacket) error {
	msg, err := p.Decode()
	if err != nil {
		return err
	}
	switch msg := msg.(type) {
	case adc.ChatMessage:
		// TODO: ADC: maybe hub should take a AAAA SID for itself
		//       and this will become B-MSG AAAA, instead of I-MSG
		fmt.Printf("%s\n", msg.Text)
		return nil
	case adc.Disconnect:
		// TODO: ADC flaw: this should be B-QUI, not I-QUI
		//  	 it always includes a SID and is, in fact, a broadcast
		return c.peerQuit(msg.ID)
	default:
		log.Printf("unhandled info command: %v", p.Name)
		return nil
	}
}

func (c *Conn) handleFeature(cmd *adc.FeaturePacket) error {
	// TODO: ADC protocol: this is another B-XXX command, but with a feature selector
	//		 might be a good idea to extend selector with some kind of tags
	//		 it may work for extensions, geo regions, chat channels, etc

	// TODO: ADC flaw: shouldn't the hub convert F-XXX to B-XXX if the current client
	//		 supports all listed extensions? does the client care about the selector?

	for fea, want := range cmd.Features {
		if _, enabled := c.ext[fea]; enabled != want {
			return nil
		}
	}

	// FIXME: this allows F-MSG that we should probably avoid
	return c.handleBroadcast(&adc.BroadcastPacket{
		ID: cmd.ID, BasePacket: cmd.BasePacket,
	})
}

func (c *Conn) handleDirect(cmd *adc.DirectPacket) error {
	msg, err := cmd.Decode()
	if err != nil {
		return err
	}
	switch msg := msg.(type) {
	case adc.ConnectRequest:
		c.revConn.Lock()
		tok, ok := c.revConn.tokens[msg.Token]
		delete(c.revConn.tokens, msg.Token)
		c.revConn.Unlock()
		if !ok {
			// TODO: handle a direct connection request from peers
			log.Printf("ignoring connection attempt from %v", cmd.ID)
			return nil
		}
		p := c.peerBySID(cmd.ID)
		go c.handleConnReq(p, tok, msg)
		return nil
	default:
		log.Printf("unhandled direct command: %v", cmd.Name)
		return nil
	}
}

func (c *Conn) handleConnReq(p *Peer, tok revConnToken, s adc.ConnectRequest) {
	if p == nil {
		tok.errc <- ErrPeerOffline
		return
	}
	if s.Proto != adc.ProtoADC {
		tok.errc <- fmt.Errorf("unsupported protocol: %q", s.Proto)
		return
	}
	if s.Port == 0 {
		tok.errc <- errors.New("no port to connect to")
		return
	}
	addr := p.Info().Ip4
	if addr == "" {
		tok.errc <- errors.New("no address to connect to")
		return
	}
	tok.addr <- addr + ":" + strconv.Itoa(s.Port)
}

func (c *Conn) handleSearch(p *Peer, data []byte) {
	var sch adc.SearchRequest
	if err := adc.Unmarshal(data, &sch); err != nil {
		log.Println("failed to decode search:", err)
		return
	}
	log.Printf("search: %+v", sch)
}

func (c *Conn) OnlinePeers() []*Peer {
	c.peers.RLock()
	defer c.peers.RUnlock()
	arr := make([]*Peer, 0, len(c.peers.bySID))
	for _, p := range c.peers.bySID {
		arr = append(arr, p)
	}
	return arr
}
func (c *Conn) peerBySID(sid adc.SID) *Peer {
	c.peers.RLock()
	p := c.peers.bySID[sid]
	c.peers.RUnlock()
	return p
}

func (c *Conn) peerJoins(sid adc.SID, u adc.User) *Peer {
	c.peers.Lock()
	defer c.peers.Unlock()
	if c.peers.byCID == nil {
		c.peers.bySID = make(map[adc.SID]*Peer)
		c.peers.byCID = make(map[adc.CID]*Peer)
	}
	p, ok := c.peers.byCID[u.Id]
	if ok {
		c.peers.bySID[sid] = p
		p.online(sid)
		p.update(u)
		return p
	}
	p = &Peer{hub: c, user: &u}
	c.peers.bySID[sid] = p
	c.peers.byCID[u.Id] = p
	p.online(sid)
	return p
}

func (c *Conn) peerQuit(sid adc.SID) error {
	c.peers.Lock()
	defer c.peers.Unlock()
	p := c.peers.bySID[sid]
	if p == nil {
		return fmt.Errorf("unknown user quits: %v", sid)
	}
	p.offline()
	delete(c.peers.bySID, sid)
	return nil
}

func (c *Conn) peerUpdate(sid adc.SID, data []byte) error {
	c.peers.Lock()
	p, ok := c.peers.bySID[sid]
	if ok {
		c.peers.Unlock()

		p.mu.Lock()
		defer p.mu.Unlock()
		return adc.Unmarshal(data, p.user)
	}
	defer c.peers.Unlock()

	var u adc.User
	if err := adc.Unmarshal(data, &u); err != nil {
		return err
	}
	if c.peers.byCID == nil {
		c.peers.bySID = make(map[adc.SID]*Peer)
		c.peers.byCID = make(map[adc.CID]*Peer)
	}
	p = &Peer{hub: c, user: &u}
	c.peers.bySID[sid] = p
	c.peers.byCID[u.Id] = p
	p.online(sid)
	return nil
}

func (c *Conn) acceptUsersList() error {
	// https://adc.sourceforge.io/ADC.html#_identify

	deadline := time.Now().Add(time.Minute)
	// Accept commands in the following order:
	// 1) Hub info (I-INF)
	// 2) Status (I-STA, optional)
	// 3) User info (B-INF, xN)
	// 3.1) Our own info (B-INF)
	const (
		hubInfo = iota
		optStatus
		userList
	)
	stage := hubInfo
	for {
		cmd, err := c.conn.ReadPacket(deadline)
		if err != nil {
			return err
		}
		switch cmd := cmd.(type) {
		case *adc.InfoPacket:
			switch stage {
			case hubInfo:
				// waiting for hub info
				if cmd.Name != (adc.User{}).Cmd() {
					return fmt.Errorf("expected hub info, received: %#v", cmd)
				}
				if err := adc.Unmarshal(cmd.Data, &c.hub); err != nil {
					return err
				}
				stage = optStatus
			case optStatus:
				// optionally wait for status command
				if cmd.Name != (adc.Status{}).Cmd() {
					return fmt.Errorf("expected status, received: %#v", cmd)
				}
				var st adc.Status
				if err := adc.Unmarshal(cmd.Data, &st); err != nil {
					return err
				} else if !st.Ok() {
					return st.Err()
				}
				stage = userList
			default:
				return fmt.Errorf("unexpected command in stage %d: %#v", stage, cmd)
			}
		case *adc.BroadcastPacket:
			switch stage {
			case optStatus:
				stage = userList
				fallthrough
			case userList:
				if cmd.ID == c.sid {
					// make sure to wipe PID, so we don't send it later occasionally
					c.user.Pid = nil
					if err := adc.Unmarshal(cmd.Data, &c.user); err != nil {
						return err
					}
					// done, should switch to NORMAL
					return nil
				}
				// other users
				var u adc.User
				if err := adc.Unmarshal(cmd.Data, &u); err != nil {
					return err
				}
				_ = c.peerJoins(cmd.ID, u)
				// continue until we see ourselves in the list
			default:
				return fmt.Errorf("unexpected command in stage %d: %#v", stage, cmd)
			}
		default:
			return fmt.Errorf("unexpected command: %#v", cmd)
		}
	}
}

type Peer struct {
	hub *Conn

	mu   sync.RWMutex
	sid  *adc.SID // may change if user disconnects
	user *adc.User
}

func (p *Peer) online(sid adc.SID) {
	p.mu.Lock()
	p.sid = &sid
	p.mu.Unlock()
}

func (p *Peer) offline() {
	p.mu.Lock()
	p.sid = nil
	p.mu.Unlock()
}

func (p *Peer) getSID() *adc.SID {
	p.mu.RLock()
	sid := p.sid
	p.mu.RUnlock()
	return sid
}

func (p *Peer) Online() bool {
	return p.getSID() != nil
}

func (p *Peer) Info() adc.User {
	p.mu.RLock()
	user := *p.user
	p.mu.RUnlock()
	return user
}

func (p *Peer) update(u adc.User) {
	p.mu.Lock()
	if p.user.Id != u.Id {
		p.mu.Unlock()
		panic("wrong cid")
	}
	*p.user = u
	p.mu.Unlock()
}

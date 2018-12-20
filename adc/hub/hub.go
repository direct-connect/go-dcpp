package hub

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dennwc/go-dcpp/adc"
	"github.com/dennwc/go-dcpp/adc/types"
)

func NewHub(info adc.HubInfo) *Hub {
	if info.Version == "" {
		info.Version = "go-dcpp 0.1"
	}
	return &Hub{info: info}
}

type Hub struct {
	lastSID uint32
	info    adc.HubInfo

	peers struct {
		sync.RWMutex
		bySID  map[adc.SID]*Conn
		byCID  map[adc.CID]*Conn
		byName map[string]*Conn
	}
}

func (h *Hub) nextSID() adc.SID {
	// TODO: reuse SIDs
	v := atomic.AddUint32(&h.lastSID, 1)
	return types.SIDFromInt(v)
}

func (h *Hub) Serve(conn net.Conn) error {
	c, err := adc.NewConn(conn)
	if err != nil {
		return err
	}
	defer c.Close()

	peer, err := h.runProtocol(c)
	if err != nil {
		return err
	}
	// connection is not yet valid and we haven't added the client to the hub yet
	if err := h.runIdentity(peer); err != nil {
		return err
	}
	log.Println("connected:", peer.sid, peer.conn.RemoteAddr(), peer.Info().Name)
	// peer registered, now we can start serving things
	defer peer.Close()

	if err = h.sendMOTD(peer); err != nil {
		return err
	}

	return h.servePeer(peer)
}

func (h *Hub) servePeer(peer *Conn) error {
	for {
		p, err := peer.conn.ReadPacket(time.Time{})
		if err == io.EOF {
			sid := peer.sid
			_ = peer.Close()
			_ = h.broadcastInfoMsg(adc.Disconnect{ID: sid})
			log.Println("disconnected:", peer.sid, peer.conn.RemoteAddr(), peer.Info().Name)
			return nil
		} else if err != nil {
			return err
		}
		switch p := p.(type) {
		case *adc.BroadcastPacket:
			if peer.sid != p.ID {
				return fmt.Errorf("malformed broadcast")
			}
			// TODO: read INF, update peer info
			// TODO: update nick, make sure there is no duplicates
			// TODO: disallow STA and some others
			go h.broadcast(p)
		case *adc.EchoPacket:
			if peer.sid != p.ID {
				return fmt.Errorf("malformed echo packet")
			}
			if err := peer.conn.WritePacket(p); err != nil {
				return err
			}
			if err = peer.conn.Flush(); err != nil {
				return err
			}
			// TODO: disallow INF, STA and some others
			go h.direct((*adc.DirectPacket)(p))
		case *adc.DirectPacket:
			if peer.sid != p.ID {
				return fmt.Errorf("malformed direct packet")
			}
			// TODO: disallow INF, STA and some others
			go h.direct(p)
		}
		log.Printf("%v: %T%+v", peer.sid, p, p)
	}
}

func (h *Hub) broadcastMsg(from adc.SID, msg adc.Message) error {
	data, err := adc.Marshal(msg)
	if err != nil {
		return err
	}
	p := &adc.BroadcastPacket{
		BasePacket: adc.BasePacket{
			Name: msg.Cmd(),
			Data: data,
		},
		ID: from,
	}
	return h.broadcast(p)
}

func (h *Hub) broadcastInfoMsg(msg adc.Message) error {
	data, err := adc.Marshal(msg)
	if err != nil {
		return err
	}
	p := &adc.InfoPacket{
		BasePacket: adc.BasePacket{
			Name: msg.Cmd(),
			Data: data,
		},
	}
	return h.broadcast(p)
}

func (h *Hub) broadcast(p adc.Packet) error {
	h.peers.RLock()
	defer h.peers.RUnlock()
	var last error
	for _, peer := range h.peers.bySID {
		err := peer.conn.WritePacket(p)
		if err == nil {
			err = peer.conn.Flush()
		}
		if err != nil {
			last = err
		}
	}
	return last
}

func (h *Hub) direct(p *adc.DirectPacket) {
	h.peers.RLock()
	peer := h.peers.bySID[p.Targ]
	h.peers.RUnlock()
	if peer == nil {
		log.Println("unknown peer:", p.Targ)
		return
	}
	err := peer.conn.WritePacket(p)
	if err == nil {
		err = peer.conn.Flush()
	}
	if err != nil {
		log.Println("direct failed:", err)
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
		go h.Serve(conn)
	}
}

func (h *Hub) runProtocol(c *adc.Conn) (*Conn, error) {
	deadline := time.Now().Add(time.Second * 5)
	// Expect features from the client
	p, err := c.ReadPacket(deadline)
	if err != nil {
		return nil, err
	}
	hp, ok := p.(*adc.HubPacket)
	if !ok {
		return nil, fmt.Errorf("expected hub messagge, got: %#v", p)
	} else if hp.Name != (adc.Supported{}).Cmd() {
		return nil, fmt.Errorf("expected support message, got: %v", hp.Name)
	}
	var sup adc.Supported
	if err := adc.Unmarshal(hp.Data, &sup); err != nil {
		return nil, err
	}
	hubFeatures := adc.ModFeatures{
		// should always be set for ADC
		adc.FeaBASE: true,
		adc.FeaTIGR: true,
		// extensions
		adc.FeaPING: true,
	}

	mutual := hubFeatures.Intersect(sup.Features)
	if !mutual.IsSet(adc.FeaBASE) && mutual.IsSet(adc.FeaBAS0) {
		return nil, fmt.Errorf("hub does not support BASE")
	} else if !mutual.IsSet(adc.FeaTIGR) {
		return nil, fmt.Errorf("hub does not support TIGR")
	}

	// send features supported by the hub
	err = c.WriteInfoMsg(adc.Supported{
		Features: hubFeatures,
	})
	if err != nil {
		return nil, err
	}
	// and allocate a SID for the client
	sid := h.nextSID()
	err = c.WriteInfoMsg(adc.SIDAssign{
		SID: sid,
	})
	if err != nil {
		return nil, err
	}
	err = c.Flush()
	if err != nil {
		return nil, err
	}
	return &Conn{hub: h, conn: c, sid: sid, fea: mutual}, nil
}

func (h *Hub) runIdentity(peer *Conn) error {
	deadline := time.Now().Add(time.Second * 5)
	// client should send INF with ID and PID set
	p, err := peer.conn.ReadPacket(deadline)
	if err != nil {
		return err
	}
	b, ok := p.(*adc.BroadcastPacket)
	if !ok {
		return fmt.Errorf("expected user info broadcast, got %#v", p)
	} else if b.Name != (adc.User{}).Cmd() {
		return fmt.Errorf("expected user info message, got %v", b.Name)
	}
	var u adc.User
	if err := adc.Unmarshal(b.Data, &u); err != nil {
		return err
	}
	if u.Id != u.Pid.Hash() {
		err = errors.New("invalid pid supplied")
		_ = peer.sendError(adc.Fatal, 27, err)
		return err
	}
	u.Pid = nil
	if u.Name == "" {
		err = errors.New("invalid nick")
		_ = peer.sendError(adc.Fatal, 21, err)
		return err
	}
	h.peers.RLock()
	_, sameName := h.peers.byName[u.Name]
	_, sameCID := h.peers.byCID[u.Id]
	h.peers.RUnlock()
	if sameName {
		err = errors.New("nick taken")
		_ = peer.sendError(adc.Fatal, 22, err)
		return err
	}
	if sameCID {
		err = errors.New("CID taken")
		_ = peer.sendError(adc.Fatal, 24, err)
		return err
	}

	if u.Ip4 == "" {
		ip := peer.conn.RemoteAddr().String()
		u.Ip4 = ip
	}
	peer.user = u

	// send hub info
	err = peer.conn.WriteInfoMsg(h.info)
	if err != nil {
		return err
	}
	// send OK status
	err = peer.conn.WriteInfoMsg(adc.Status{
		Sev:  adc.Success,
		Code: 0,
		Msg:  "powered by Gophers",
	})
	// send user list
	h.peers.RLock()
	for sid, p := range h.peers.bySID {
		err = peer.conn.WriteBroadcast(sid, p.Info())
		if err != nil {
			h.peers.RUnlock()
			return err
		}
	}
	h.peers.RUnlock()
	// finally accept user on the hub
	h.peers.Lock()
	if h.peers.bySID == nil {
		h.peers.bySID = make(map[adc.SID]*Conn)
		h.peers.byCID = make(map[adc.CID]*Conn)
		h.peers.byName = make(map[string]*Conn)
	}
	h.peers.bySID[peer.sid] = peer
	h.peers.byCID[u.Id] = peer
	h.peers.byName[u.Name] = peer
	h.peers.Unlock()
	// write his info and flush
	_ = h.broadcastMsg(peer.sid, u)
	return nil
}

func (h *Hub) sendMOTD(peer *Conn) error {
	err := peer.conn.WriteInfoMsg(adc.ChatMessage{
		Text: "Welcome!",
	})
	if err != nil {
		return err
	}
	err = peer.conn.Flush()
	if err != nil {
		return err
	}
	return nil
}

type Conn struct {
	hub  *Hub
	conn *adc.Conn
	fea  adc.ModFeatures
	sid  adc.SID

	mu   sync.Mutex
	user adc.User
}

func (c *Conn) sendOneMsg(m adc.Message) error {
	err := c.conn.WriteInfoMsg(m)
	if err != nil {
		return err
	}
	return c.conn.Flush()
}

func (c *Conn) sendError(sev adc.Severity, code int, err error) error {
	return c.sendOneMsg(adc.Status{
		Sev: sev, Code: code, Msg: err.Error(),
	})
}

func (c *Conn) Info() adc.User {
	return c.user
}

func (c *Conn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	err := c.conn.Close()

	c.hub.peers.Lock()
	delete(c.hub.peers.bySID, c.sid)
	delete(c.hub.peers.byName, c.user.Name)
	delete(c.hub.peers.byCID, c.user.Id)
	c.hub.peers.Unlock()

	return err
}

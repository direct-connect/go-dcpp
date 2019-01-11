package hub

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/dennwc/go-dcpp/nmdc"
)

type Info struct {
	Name  string
	Topic string
}

func NewHub(info Info) *Hub {
	return &Hub{info: info}
}

type Hub struct {
	info Info

	peers struct {
		sync.RWMutex
		logging map[nmdc.Name]struct{}
		byName  map[nmdc.Name]*Conn
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
			err := h.Serve(conn)
			if err != nil {
				log.Println(err)
			}
		}()
	}
}

func (h *Hub) Serve(conn net.Conn) error {
	c, err := nmdc.NewConn(conn)
	if err != nil {
		return err
	}

	peer, err := h.handshake(c)
	if err != nil {
		c.Close()
		return err
	}
	defer peer.Close()
	return h.servePeer(peer)
}

func (h *Hub) handshake(c *nmdc.Conn) (*Conn, error) {
	// TODO: randomize
	const lock = "EXTENDEDPROTOCOL_godcpp"
	err := c.WriteMsg(&nmdc.Lock{
		Lock: lock,
		PK:   "go-dcpp_nmdc_hub",
	})
	if err != nil {
		return nil, err
	}
	err = c.Flush()
	if err != nil {
		return nil, err
	}

	deadline := time.Now().Add(time.Second * 5)
	msg, err := c.ReadMsg(deadline)
	if err != nil {
		return nil, fmt.Errorf("expected supports: %v", err)
	}
	sup, ok := msg.(*nmdc.Supports)
	if !ok {
		return nil, fmt.Errorf("expected supports from the client, got: %#v", msg)
	}
	msg, err = c.ReadMsg(deadline)
	if err != nil {
		return nil, fmt.Errorf("expected key: %v", err)
	}
	key, ok := msg.(*nmdc.Key)
	if !ok {
		return nil, fmt.Errorf("expected key from the client, got: %#v", msg)
	} else if key.Key != nmdc.Unlock(lock) {
		return nil, errors.New("wrong key")
	}
	our := nmdc.Features{
		nmdc.FeaNoHello:   {},
		nmdc.FeaNoGetINFO: {},
	}
	mutual := our.IntersectList(sup.Ext)
	if _, ok := mutual[nmdc.FeaNoHello]; !ok {
		return nil, errors.New("NoHello is not supported")
	} else if _, ok := mutual[nmdc.FeaNoGetINFO]; !ok {
		return nil, errors.New("FeaNoGetINFO is not supported")
	}
	msg, err = c.ReadMsg(deadline)
	if err != nil {
		return nil, fmt.Errorf("expected validate: %v", err)
	}
	nick, ok := msg.(*nmdc.ValidateNick)
	if !ok {
		return nil, fmt.Errorf("expected validate from the client, got: %#v", msg)
	}
	if nick.Name == "" {
		return nil, errors.New("empty nickname")
	}

	peer := &Conn{
		hub:  h,
		conn: c,
		fea:  mutual,
		name: nick.Name,
	}

	h.peers.Lock()
	_, ok = h.peers.logging[nick.Name]
	if !ok {
		_, ok = h.peers.byName[nick.Name]
	}
	if ok {
		h.peers.Unlock()
		return nil, errors.New("nickname is taken")
	}
	if h.peers.logging == nil {
		h.peers.logging = make(map[nmdc.Name]struct{})
	}
	h.peers.logging[nick.Name] = struct{}{}
	h.peers.Unlock()

	err = h.accept(peer, our)
	if err != nil {
		h.peers.Lock()
		delete(h.peers.logging, nick.Name)
		h.peers.Unlock()
		return nil, err
	}
	return peer, nil
}

func (h *Hub) accept(peer *Conn, our nmdc.Features) error {
	deadline := time.Now().Add(time.Second * 5)

	c := peer.conn
	err := c.WriteMsg(&nmdc.Supports{
		Ext: our.List(),
	})
	if err != nil {
		return err
	}
	err = c.WriteMsg(&nmdc.HubName{
		Name: nmdc.Name(h.info.Name),
	})
	if err != nil {
		return err
	}
	err = c.WriteMsg(&nmdc.Hello{
		Name: peer.name,
	})
	if err != nil {
		return err
	}
	err = c.Flush()
	if err != nil {
		return err
	}

	msg, err := c.ReadMsg(deadline)
	if err != nil {
		return fmt.Errorf("expected version: %v", err)
	}
	vers, ok := msg.(*nmdc.Version)
	if !ok {
		return fmt.Errorf("expected version from the client, got: %#v", msg)
	} else if vers.Vers != "1,0091" && vers.Vers != "1.0091" {
		return fmt.Errorf("unexpected version: %q", vers)
	}
	msg, err = c.ReadMsg(deadline)
	if err != nil {
		return fmt.Errorf("expected version: %v", err)
	}
	_, ok = msg.(*nmdc.GetNickList)
	if !ok {
		return fmt.Errorf("expected nick list request from the client, got: %#v", msg)
	}
	msg, err = c.ReadMsg(deadline)
	if err != nil {
		return fmt.Errorf("expected user info: %v", err)
	}
	user, ok := msg.(*nmdc.MyInfo)
	if !ok {
		return fmt.Errorf("expected user info from the client, got: %#v", msg)
	} else if user.Name != peer.name {
		return errors.New("nick missmatch")
	}
	peer.user = *user

	err = c.WriteMsg(&peer.user)
	if err != nil {
		return err
	}
	err = c.WriteMsg(&nmdc.HubTopic{
		Text: h.info.Topic,
	})
	if err != nil {
		return err
	}
	err = h.peerLogin(peer)
	if err != nil {
		return err
	}
	return nil
}

func (h *Hub) peerLogin(peer *Conn) error {
	user := peer.Info()
	h.peers.Lock()
	delete(h.peers.logging, user.Name)
	list := make([]*Conn, 0, len(h.peers.byName))
	for _, p := range h.peers.byName {
		list = append(list, p)
	}
	if h.peers.byName == nil {
		h.peers.byName = make(map[nmdc.Name]*Conn)
	}
	h.peers.byName[user.Name] = peer
	h.peers.Unlock()
	for _, p := range list {
		info := p.Info()
		// send this peer info to a new peer
		err := peer.conn.WriteMsg(&info)
		if err != nil {
			h.peerLogout(peer)
			return err
		}
		// send new peer info to this peer
		_ = p.WriteOne(&user)
	}
	err := peer.WriteOne(&user)
	if err != nil {
		h.peerLogout(peer)
		return err
	}
	return nil
}

func (h *Hub) peerLogout(peer *Conn) {
	name := peer.Name()
	h.peers.Lock()
	delete(h.peers.byName, name)
	list := make([]*Conn, 0, len(h.peers.byName))
	for _, p := range h.peers.byName {
		list = append(list, p)
	}
	h.peers.Unlock()
	for _, p := range list {
		_ = p.WriteOne(&nmdc.Quit{Name: name})
	}
}

func (h *Hub) servePeer(peer *Conn) error {
	for {
		msg, err := peer.conn.ReadMsg(time.Time{})
		if err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}
		// TODO
		log.Println(msg)
	}
}

type Conn struct {
	hub  *Hub
	conn *nmdc.Conn
	fea  nmdc.Features

	name nmdc.Name
	mu   sync.Mutex
	user nmdc.MyInfo
}

func (c *Conn) WriteOne(msg nmdc.Message) error {
	err := c.conn.WriteMsg(msg)
	if err != nil {
		return err
	}
	return c.conn.Flush()
}

func (c *Conn) Name() nmdc.Name {
	return c.name
}

func (c *Conn) Info() nmdc.MyInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.user
}

func (c *Conn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	err := c.conn.Close()

	c.hub.peerLogout(c)

	return err
}

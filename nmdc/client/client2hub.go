package client

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/dennwc/go-dcpp/nmdc"
)

// DialHub connects to a hub and runs a handshake.
func DialHub(addr string, info *Config) (*Conn, error) {
	if !strings.Contains(addr, ":") {
		addr += ":411"
	}
	conn, err := nmdc.Dial(addr)
	if err != nil {
		return nil, err
	}
	return HubHandshake(conn, info)
}

type Config struct {
	Name string
	Ext  []string
}

func (c *Config) validate() error {
	if c.Name == "" {
		return errors.New("name should be set")
	}
	return nil
}

// HubHandshake begins a Client-Hub handshake on a connection.
func HubHandshake(conn *nmdc.Conn, conf *Config) (*Conn, error) {
	if err := conf.validate(); err != nil {
		return nil, err
	}
	mutual, err := hubHanshake(conn, conf)
	if err != nil {
		conn.Close()
		return nil, err
	}
	c := &Conn{
		conn:    conn,
		fea:     mutual,
		closing: make(chan struct{}),
		closed:  make(chan struct{}),
	}
	c.user.Name = nmdc.Name(conf.Name)
	c.peers.byName = make(map[nmdc.Name]*Peer)
	if err = initConn(c); err != nil {
		conn.Close()
		return nil, err
	}
	go c.readLoop()
	return c, nil
}

func hubHanshake(conn *nmdc.Conn, conf *Config) (nmdc.Features, error) {
	deadline := time.Now().Add(time.Second * 5)
	// TODO: optimize for Lock
	msg, err := conn.ReadMsg(deadline)
	if err != nil {
		return nil, fmt.Errorf("expected lock: %v", err)
	}
	lock, ok := msg.(*nmdc.Lock)
	if !ok {
		return nil, fmt.Errorf("expected lock from the server, got: %#v", msg)
	}
	if !strings.HasPrefix(lock.Lock, "EXTENDEDPROTOCOL") {
		// TODO: support legacy protocol, if we care
		return nil, errors.New("extensions are not supported by hub")
	}

	ext := []string{
		nmdc.FeaNoHello,
		"BotList",
		"BZList",
		//"ADCGet",
		//"XmlBZList",
	}
	ext = append(ext, conf.Ext...)

	err = conn.WriteMsg(&nmdc.Supports{Ext: ext})
	if err != nil {
		return nil, err
	}
	err = conn.WriteMsg(&nmdc.Key{Key: unlock(lock.Lock)})
	if err != nil {
		return nil, err
	}
	err = conn.WriteMsg(&nmdc.ValidateNick{Name: nmdc.Name(conf.Name)})
	if err != nil {
		return nil, err
	}
	err = conn.Flush()
	if err != nil {
		return nil, err
	}
	deadline = deadline.Add(time.Second * 5)

	our := make(nmdc.Features)
	for _, e := range ext {
		our.Set(e)
	}
	var mutual nmdc.Features

handshake:
	for {
		msg, err = conn.ReadMsg(deadline)
		if err != nil {
			return nil, err
		}
		switch msg := msg.(type) {
		case *nmdc.ChatMessage:
			// TODO: it seems like the server may send MOTD before replying with $Supports
			//		 we need to queue those messages and replay them once connection is established

			// skip for now
		case *nmdc.Supports:
			mutual = our.IntersectList(msg.Ext)
			if _, ok := mutual[nmdc.FeaNoHello]; !ok {
				// TODO: support hello as well
				return nil, fmt.Errorf("no hello is not supported: %v", msg.Ext)
			}
		case *nmdc.Hello:
			if string(msg.Name) != conf.Name {
				return nil, fmt.Errorf("unexpected name in hello: %q", msg.Name)
			}
			break handshake
		default:
			// TODO: HubName, GetPass, ...?
			return nil, fmt.Errorf("unexpected command in handshake: %#v", msg)
		}
	}

	err = conn.WriteMsg(&nmdc.Version{Vers: "1,0091"})
	if err != nil {
		return nil, err
	}
	err = conn.WriteMsg(&nmdc.GetNickList{})
	if err != nil {
		return nil, err
	}
	err = conn.WriteMsg(&nmdc.MyInfo{
		Name: nmdc.Name(conf.Name),
		// TODO
		Tag:  "Go V:0.1,M:P,H:0/1/0,S:2",
		Info: "$LAN(T3)0x31$example@example.com$12345$",
	})
	if err != nil {
		return nil, err
	}
	err = conn.Flush()
	if err != nil {
		return nil, err
	}
	return mutual, nil
}

func initConn(c *Conn) error {
	deadline := time.Now().Add(time.Second * 30)
	for {
		msg, err := c.conn.ReadMsg(deadline)
		if err != nil {
			return err
		}
		switch msg := msg.(type) {
		case *nmdc.HubName:
			c.hub.Name = msg.Name
		case *nmdc.HubTopic:
			c.hub.Topic = msg.Text
		case *nmdc.MyInfo:
			if msg.Name == c.user.Name {
				c.user = *msg
				return nil
			}
			if _, ok := c.peers.byName[msg.Name]; ok {
				return errors.New("duplicate user in the list")
			}
			peer := &Peer{hub: c, info: *msg}
			c.peers.byName[msg.Name] = peer
		default:
			return fmt.Errorf("unexpected command: %#v", msg)
		}
	}
}

var keyReplace = map[byte]string{
	0:   "/%DCN000%/",
	5:   "/%DCN005%/",
	36:  "/%DCN036%/",
	96:  "/%DCN096%/",
	124: "/%DCN124%/",
	126: "/%DCN126%/",
}

func unlock(str string) string {
	lock := []byte(str)

	n := len(lock)
	key := make([]byte, n)

	for i := 1; i < n; i++ {
		key[i] = (lock[i] ^ lock[i-1]) & 0xFF
	}
	key[0] = byte((((lock[0] ^ lock[n-1]) ^ lock[n-2]) ^ 5) & 0xFF)
	for i := 0; i < n; i++ {
		key[i] = byte((((key[i] << 4) & 0xF0) | ((key[i] >> 4) & 0x0F)) & 0xFF)
	}
	buf := bytes.NewBuffer(nil)
	buf.Grow(len(key))
	for _, v := range key {
		if esc, ok := keyReplace[v]; ok {
			buf.WriteString(esc)
		} else {
			buf.WriteByte(v)
		}
	}
	return buf.String()
}

// Conn represents a Client-to-Hub connection.
type Conn struct {
	conn *nmdc.Conn
	fea  nmdc.Features

	closing chan struct{}
	closed  chan struct{}

	imu  sync.RWMutex
	user nmdc.MyInfo
	hub  HubInfo

	peers struct {
		sync.RWMutex
		byName map[nmdc.Name]*Peer
	}
}

func (c *Conn) HubInfo() HubInfo {
	c.imu.RLock()
	h := c.hub
	c.imu.RUnlock()
	return h
}

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

func (c *Conn) OnlinePeers() []*Peer {
	c.peers.RUnlock()
	defer c.peers.RUnlock()
	list := make([]*Peer, 0, len(c.peers.byName))
	for _, peer := range c.peers.byName {
		list = append(list, peer)
	}
	return list
}

func (c *Conn) readLoop() {
	defer close(c.closed)
	for {
		msg, err := c.conn.ReadMsg(time.Time{})
		if err != nil {
			log.Println(err)
			return
		}
		switch msg := msg.(type) {
		case *nmdc.HubName:
			c.imu.Lock()
			c.hub.Name = msg.Name
			c.imu.Unlock()
		case *nmdc.HubTopic:
			c.imu.Lock()
			c.hub.Topic = msg.Text
			c.imu.Unlock()
		case *nmdc.ChatMessage:
			if msg.Name != "" {
				fmt.Printf("%s\n", msg.Text)
			} else {
				fmt.Printf("<%s> %s\n", msg.Name, msg.Text)
			}
		case *nmdc.OpList:
			c.peers.RLock()
			for _, name := range msg.List {
				p := c.peers.byName[name]
				if p == nil {
					log.Printf("op user does not exist: %q", name)
					return
				}
				p.mu.Lock()
				p.op = true
				p.mu.Unlock()
			}
			c.peers.RUnlock()
		case *nmdc.BotList:
			c.peers.RLock()
			for _, name := range msg.List {
				p := c.peers.byName[name]
				if p == nil {
					log.Printf("bot user does not exist: %q", name)
					return
				}
				p.mu.Lock()
				p.bot = true
				p.mu.Unlock()
			}
			c.peers.RUnlock()
		default:
			log.Printf("unhandled command: %T %+v", msg, msg)
		}
	}
}

type HubInfo struct {
	Name  nmdc.Name
	Topic string
}

type Peer struct {
	hub *Conn

	mu   sync.RWMutex
	info nmdc.MyInfo
	op   bool
	bot  bool
}

func (p *Peer) IsOp() bool {
	p.mu.RLock()
	v := p.op
	p.mu.RUnlock()
	return v
}

func (p *Peer) IsBot() bool {
	p.mu.RLock()
	v := p.bot
	p.mu.RUnlock()
	return v
}

func (p *Peer) Info() nmdc.MyInfo {
	p.mu.RLock()
	u := p.info
	p.mu.RUnlock()
	return u
}

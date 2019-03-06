package client

import (
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/direct-connect/go-dcpp/nmdc"
	"github.com/direct-connect/go-dcpp/version"
)

// DialHub connects to a hub and runs a handshake.
func DialHub(addr string, info *Config) (*Conn, error) {
	if !strings.Contains(addr, ":") {
		addr += ":411"
	}
	// TODO: use context
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
	mutual, hub, err := hubHanshake(conn, conf)
	if err != nil {
		conn.Close()
		return nil, err
	}
	c := &Conn{
		conn:    conn,
		fea:     mutual,
		hub:     *hub,
		closing: make(chan struct{}),
		closed:  make(chan struct{}),
	}
	c.user.Name = nmdc.Name(conf.Name)
	c.peers.byName = make(map[nmdc.Name]*Peer)
	if err = initConn(c); err != nil {
		conn.Close()
		return nil, err
	}
	conn.KeepAlive(time.Minute / 2)
	go c.readLoop()
	return c, nil
}

func hubHanshake(conn *nmdc.Conn, conf *Config) (nmdc.Features, *HubInfo, error) {
	deadline := time.Now().Add(time.Second * 5)

	ext := []string{
		nmdc.FeaNoHello,
		nmdc.FeaNoGetINFO,
	}
	ext = append(ext, conf.Ext...)

	_, err := conn.SendClientHandshake(deadline, conf.Name, ext...)
	if err != nil {
		return nil, nil, err
	}
	deadline = deadline.Add(time.Second * 5)

	our := make(nmdc.Features)
	for _, e := range ext {
		our.Set(e)
	}
	var (
		mutual nmdc.Features
		hub    HubInfo
	)

handshake:
	for {
		msg, err := conn.ReadMsg(deadline)
		if err == io.EOF {
			return nil, nil, io.ErrUnexpectedEOF
		} else if err != nil {
			return nil, nil, err
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
				return nil, nil, fmt.Errorf("no hello is not supported: %v", msg.Ext)
			}
		case *nmdc.HubName:
			hub.Name = msg.String
		case *nmdc.Hello:
			if string(msg.Name) != conf.Name {
				return nil, nil, fmt.Errorf("unexpected name in hello: %q", msg.Name)
			}
			break handshake
		default:
			// TODO: HubTopic, GetPass, ...?
			return nil, nil, fmt.Errorf("unexpected command in handshake: %#v", msg)
		}
	}

	err = conn.SendClientInfo(deadline, &nmdc.MyInfo{
		Name:    nmdc.Name(conf.Name),
		Client:  version.Name,
		Version: version.Vers,
		Flag:    nmdc.FlagStatusNormal | nmdc.FlagIPv4 | nmdc.FlagTLSDownload,

		// TODO
		Mode:       nmdc.UserModePassive,
		HubsNormal: 1,
		Slots:      1,
		Conn:       "LAN(T3)",
		ShareSize:  13 * 1023 * 1023 * 1023,
	})
	if err != nil {
		return nil, nil, err
	}
	return mutual, &hub, nil
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
			c.hub.Name = msg.String
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
	on struct {
		chat      func(m *nmdc.ChatMessage) error
		unhandled func(m nmdc.Message) error
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

func (c *Conn) OnChatMessage(fnc func(m *nmdc.ChatMessage) error) {
	c.on.chat = fnc
}

func (c *Conn) OnUnhandled(fnc func(m nmdc.Message) error) {
	c.on.unhandled = fnc
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

func (c *Conn) SendChatMsg(msg string) error {
	c.imu.RLock()
	name := c.user.Name
	c.imu.RUnlock()
	return c.conn.WriteOneMsg(&nmdc.ChatMessage{
		Name: name, Text: msg,
	})
}

func (c *Conn) readLoop() {
	defer close(c.closed)
	for {
		msg, err := c.conn.ReadMsg(time.Time{})
		if err == io.EOF {
			return
		} else if err != nil {
			log.Println("read msg:", err)
			return
		}
		switch msg := msg.(type) {
		case *nmdc.HubName:
			c.imu.Lock()
			c.hub.Name = msg.String
			c.imu.Unlock()
		case *nmdc.HubTopic:
			c.imu.Lock()
			c.hub.Topic = msg.Text
			c.imu.Unlock()
		case *nmdc.ChatMessage:
			if c.on.chat != nil {
				if err = c.on.chat(msg); err != nil {
					log.Println("chat msg:", err)
					return
				}
			}
		case *nmdc.OpList:
			c.peers.RLock()
			for _, name := range msg.Names {
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
			for _, name := range msg.Names {
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
			if c.on.unhandled != nil {
				if err = c.on.unhandled(msg); err != nil {
					log.Println("unhandled msg:", err)
					return
				}
			}
		}
	}
}

type HubInfo struct {
	Name  nmdc.String
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

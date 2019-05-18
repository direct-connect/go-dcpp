package client

import (
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	nmdcp "github.com/direct-connect/go-dc/nmdc"
	"github.com/direct-connect/go-dc/types"
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
	c.user.Name = conf.Name
	c.peers.byName = make(map[string]*Peer)
	if err = initConn(c); err != nil {
		conn.Close()
		return nil, err
	}
	//conn.KeepAlive(time.Minute / 2)
	go c.readLoop()
	return c, nil
}

func hubHanshake(conn *nmdc.Conn, conf *Config) (nmdcp.Extensions, *HubInfo, error) {
	deadline := time.Now().Add(time.Second * 5)

	ext := []string{
		nmdcp.ExtNoHello,
		nmdcp.ExtNoGetINFO,
	}
	ext = append(ext, conf.Ext...)

	_, err := conn.SendClientHandshake(deadline, ext...)
	if err != nil {
		return nil, nil, err
	}
	deadline = deadline.Add(time.Second * 5)

	our := make(nmdcp.Extensions)
	for _, e := range ext {
		our.Set(e)
	}
	var (
		mutual nmdcp.Extensions
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
		case *nmdcp.ChatMessage:
			// TODO: it seems like the server may send MOTD before replying with $Supports
			//		 we need to queue those messages and replay them once connection is established

			// skip for now
		case *nmdcp.Supports:
			mutual = our.IntersectList(msg.Ext)
			if _, ok := mutual[nmdcp.ExtNoHello]; !ok {
				// TODO: support hello as well
				return nil, nil, fmt.Errorf("no hello is not supported: %v", msg.Ext)
			}
			err = conn.WriteOneMsg(&nmdcp.ValidateNick{Name: nmdcp.Name(conf.Name)})
			if err != nil {
				return nil, nil, err
			}
		case *nmdcp.HubName:
			hub.Name = string(msg.String)
		case *nmdcp.Hello:
			if string(msg.Name) != conf.Name {
				return nil, nil, fmt.Errorf("unexpected name in hello: %q", msg.Name)
			}
			break handshake
		default:
			// TODO: HubTopic, GetPass, ...?
			return nil, nil, fmt.Errorf("unexpected command in handshake: %#v", msg)
		}
	}

	err = conn.SendClientInfo(deadline, &nmdcp.MyINFO{
		Name: conf.Name,
		Client: types.Software{
			Name:    version.Name,
			Version: version.Vers,
		},
		Flag: nmdcp.FlagStatusNormal | nmdcp.FlagIPv4 | nmdcp.FlagTLSDownload,

		// TODO
		Mode:       nmdcp.UserModePassive,
		HubsNormal: 1,
		Slots:      1,
		Conn:       "100",
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
		case *nmdcp.HubName:
			c.hub.Name = string(msg.String)
		case *nmdcp.HubTopic:
			c.hub.Topic = msg.Text
		case *nmdcp.MyINFO:
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
	fea  nmdcp.Extensions

	closing chan struct{}
	closed  chan struct{}

	imu  sync.RWMutex
	user nmdcp.MyINFO
	hub  HubInfo

	peers struct {
		sync.RWMutex
		byName map[string]*Peer
	}
	on struct {
		chat      func(m *nmdcp.ChatMessage) error
		unhandled func(m nmdcp.Message) error
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

func (c *Conn) OnChatMessage(fnc func(m *nmdcp.ChatMessage) error) {
	c.on.chat = fnc
}

func (c *Conn) OnUnhandled(fnc func(m nmdcp.Message) error) {
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
	return c.conn.WriteOneMsg(&nmdcp.ChatMessage{
		Name: name, Text: msg,
	})
}

func (c *Conn) readLoop() {
	defer close(c.closed)
	for {
		msg, err := c.conn.ReadMsg(time.Time{})
		if err == io.EOF || err == io.ErrClosedPipe {
			return
		} else if err != nil {
			log.Println("read msg:", err)
			return
		}
		switch msg := msg.(type) {
		case *nmdcp.HubName:
			c.imu.Lock()
			c.hub.Name = string(msg.String)
			c.imu.Unlock()
		case *nmdcp.HubTopic:
			c.imu.Lock()
			c.hub.Topic = msg.Text
			c.imu.Unlock()
		case *nmdcp.ChatMessage:
			if c.on.chat != nil {
				if err = c.on.chat(msg); err != nil {
					log.Println("chat msg:", err)
					return
				}
			}
		case *nmdcp.OpList:
			c.peers.RLock()
			for _, name := range msg.Names {
				if name == "" {
					continue
				}
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
		case *nmdcp.BotList:
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
	Name  string
	Topic string
}

type Peer struct {
	hub *Conn

	mu   sync.RWMutex
	info nmdcp.MyINFO
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

func (p *Peer) Info() nmdcp.MyINFO {
	p.mu.RLock()
	u := p.info
	p.mu.RUnlock()
	return u
}

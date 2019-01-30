package hub

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/direct-connect/go-dcpp/nmdc"
)

const nmdcFakeToken = "nmdc"

func (h *Hub) ServeNMDC(conn net.Conn) error {
	log.Printf("%s: using NMDC", conn.RemoteAddr())
	c, err := nmdc.NewConn(conn)
	if err != nil {
		return err
	}
	defer c.Close()

	peer, err := h.nmdcHandshake(c)
	if err != nil {
		return err
	}
	defer peer.Close()
	return h.nmdcServePeer(peer)
}

func (h *Hub) nmdcHandshake(c *nmdc.Conn) (*nmdcPeer, error) {
	lock := &nmdc.Lock{
		Lock: "EXTENDEDPROTOCOL_godcpp", // TODO: randomize
		PK:   h.info.Soft.Name + " " + h.info.Soft.Vers,
	}
	err := c.WriteMsg(lock)
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
	} else if key.Key != lock.Key().Key {
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
		return nil, errors.New("NoGetINFO is not supported")
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

	peer := &nmdcPeer{
		BasePeer: BasePeer{
			hub:  h,
			addr: c.RemoteAddr(),
			sid:  h.nextSID(),
		},
		conn: c,
		fea:  mutual,
	}
	peer.user.Name = nick.Name
	name := string(nick.Name)

	// do not lock for writes first
	h.peers.RLock()
	_, sameName1 := h.peers.logging[name]
	_, sameName2 := h.peers.byName[name]
	h.peers.RUnlock()

	if sameName1 || sameName2 {
		_ = peer.writeOneNow(&nmdc.ValidateDenide{nick.Name})
		return nil, errNickTaken
	}

	// ok, now lock for writes and try to bind nick
	h.peers.Lock()
	_, sameName1 = h.peers.logging[name]
	_, sameName2 = h.peers.byName[name]
	if sameName1 || sameName2 {
		h.peers.Unlock()

		_ = peer.writeOneNow(&nmdc.ValidateDenide{nick.Name})
		return nil, errNickTaken
	}
	// bind nick, still no one will see us yet
	h.peers.logging[name] = struct{}{}
	h.peers.Unlock()

	err = h.nmdcAccept(peer, our)
	if err != nil {
		h.peers.Lock()
		delete(h.peers.logging, name)
		h.peers.Unlock()
		return nil, err
	}

	// finally accept the user on the hub
	h.peers.Lock()
	// cleanup temporary bindings
	delete(h.peers.logging, name)

	// make a snapshot of peers to send info to
	list := h.listPeers()

	// add user to the hub
	h.peers.bySID[peer.sid] = peer
	h.peers.byName[name] = peer
	h.peers.Unlock()

	// notify other users about the new one
	// TODO: this will block the client
	h.broadcastUserJoin(peer, list)

	return peer, nil
}

func (h *Hub) nmdcAccept(peer *nmdcPeer, our nmdc.Features) error {
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
		Name: peer.user.Name,
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
	} else if user.Name != peer.user.Name {
		return errors.New("nick missmatch")
	}
	peer.user = *user

	err = c.WriteMsg(&peer.user)
	if err != nil {
		return err
	}
	err = c.WriteMsg(&nmdc.HubTopic{
		Text: h.info.Desc,
	})
	if err != nil {
		return err
	}
	err = h.sendMOTD(peer)
	if err != nil {
		return err
	}

	// send user list (except his own info)
	err = peer.PeersJoin(h.Peers())
	if err != nil {
		return err
	}

	// write his info and flush
	err = peer.PeersJoin([]Peer{peer})
	if err != nil {
		return err
	}
	return nil
}

func (h *Hub) nmdcServePeer(peer *nmdcPeer) error {
	peer.conn.KeepAlive(time.Minute / 2)
	for {
		msg, err := peer.conn.ReadMsg(time.Time{})
		if err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}
		switch msg := msg.(type) {
		case *nmdc.ChatMessage:
			if string(msg.Name) != peer.Name() {
				return errors.New("invalid name in the chat message")
			}
			go h.broadcastChat(peer, string(msg.Text), nil)
		case *nmdc.ConnectToMe:
			targ := h.byName(string(msg.Targ))
			if targ == nil {
				continue
			}
			// TODO: token?
			go h.connectReq(peer, targ, msg.Address, nmdcFakeToken, msg.Secure)
		case *nmdc.RevConnectToMe:
			if string(msg.From) != peer.Name() {
				return errors.New("invalid name in RevConnectToMe")
			}
			targ := h.byName(string(msg.To))
			if targ == nil {
				continue
			}
			go h.revConnectReq(peer, targ, nmdcFakeToken, targ.User().TLS)
		case *nmdc.PrivateMessage:
			if string(msg.From) != peer.Name() {
				return errors.New("invalid name in PrivateMessage")
			}
			targ := h.byName(string(msg.To))
			if targ == nil {
				continue
			}
			go h.privateChat(peer, targ, string(msg.Text))
		default:
			// TODO
			data, _ := msg.MarshalNMDC()
			log.Printf("%s: nmdc: $%s %v|", peer.RemoteAddr(), msg.Cmd(), string(data))
		}
	}
}

var _ Peer = (*nmdcPeer)(nil)

type nmdcPeer struct {
	BasePeer

	conn *nmdc.Conn
	fea  nmdc.Features

	mu      sync.RWMutex
	user    nmdc.MyInfo
	closeMu sync.Mutex
	closed  bool
}

func (p *nmdcPeer) User() User {
	u := p.Info()
	return User{
		Name: string(u.Name),
		App: Software{
			Name: u.Client,
			Vers: u.Version,
		},
		Email: u.Email,
		Share: u.ShareSize,
		IPv4:  u.Flag.IsSet(nmdc.FlagIPv4),
		IPv6:  u.Flag.IsSet(nmdc.FlagIPv6),
		TLS:   u.Flag.IsSet(nmdc.FlagTLS),
	}
}

func (p *nmdcPeer) Name() string {
	p.mu.RLock()
	name := p.user.Name
	p.mu.RUnlock()
	return string(name)
}

func (p *nmdcPeer) Info() nmdc.MyInfo {
	p.mu.RLock()
	u := p.user
	p.mu.RUnlock()
	return u
}

func (p *nmdcPeer) Close() error {
	p.closeMu.Lock()
	defer p.closeMu.Unlock()
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed {
		return nil
	}
	err := p.conn.Close()
	p.closed = true

	name := string(p.user.Name)
	p.hub.leave(p, p.sid, name)
	return err
}

func (p *nmdcPeer) writeOne(msg nmdc.Message) error {
	return p.conn.WriteOneMsg(msg)
}

func (p *nmdcPeer) writeOneNow(msg nmdc.Message) error {
	if err := p.conn.WriteOneMsg(msg); err != nil {
		return err
	}
	return p.conn.Flush()
}

func (p *nmdcPeer) PeersJoin(peers []Peer) error {
	for _, peer := range peers {
		var u nmdc.MyInfo
		if p2, ok := peer.(*nmdcPeer); ok {
			u = p2.Info()
		} else {
			info := peer.User()
			flag := nmdc.FlagStatusNormal
			if info.IPv4 {
				flag |= nmdc.FlagIPv4
			}
			if info.IPv6 {
				flag |= nmdc.FlagIPv6
			}
			if info.TLS {
				flag |= nmdc.FlagTLS
			}
			u = nmdc.MyInfo{
				Name:      nmdc.Name(info.Name),
				Client:    info.App.Name,
				Version:   info.App.Vers,
				Email:     info.Email,
				ShareSize: info.Share,
				Flag:      flag,

				// TODO
				Mode:  nmdc.UserModeActive,
				Hubs:  [3]int{1, 0, 0},
				Slots: 1,
				Conn:  "LAN(T3)",
			}
		}
		if err := p.conn.WriteMsg(&u); err != nil {
			return err
		}
	}
	return p.conn.Flush()
}

func (p *nmdcPeer) PeersLeave(peers []Peer) error {
	for _, peer := range peers {
		if err := p.conn.WriteMsg(&nmdc.Quit{
			Name: nmdc.Name(peer.Name()),
		}); err != nil {
			return err
		}
	}
	return p.conn.Flush()
}

func (p *nmdcPeer) ChatMsg(from Peer, text string) error {
	return p.writeOne(&nmdc.ChatMessage{
		Name: nmdc.Name(from.Name()),
		Text: nmdc.String(text),
	})
}

func (p *nmdcPeer) PrivateMsg(from Peer, text string) error {
	return p.writeOne(&nmdc.PrivateMessage{
		To:   nmdc.Name(p.Name()),
		From: nmdc.Name(from.Name()),
		Text: nmdc.String(text),
	})
}

func (p *nmdcPeer) HubChatMsg(text string) error {
	return p.writeOne(&nmdc.ChatMessage{Text: nmdc.String(text)})
}

func (p *nmdcPeer) ConnectTo(peer Peer, addr string, token string, secure bool) error {
	// TODO: save token somewhere?
	return p.writeOne(&nmdc.ConnectToMe{
		Targ:    nmdc.Name(peer.Name()),
		Address: addr,
		Secure:  secure,
	})
}

func (p *nmdcPeer) RevConnectTo(peer Peer, token string, secure bool) error {
	// TODO: save token somewhere?
	return p.writeOne(&nmdc.RevConnectToMe{
		From: nmdc.Name(peer.Name()),
		To:   nmdc.Name(p.Name()),
	})
}

func (p *nmdcPeer) failed(text string) error {
	return p.writeOneNow(&nmdc.Failed{Text: nmdc.String(text)})
}

func (p *nmdcPeer) error(text string) error {
	return p.writeOneNow(&nmdc.Error{Text: nmdc.String(text)})
}

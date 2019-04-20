package hub

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/go-irc/irc"

	dc "github.com/direct-connect/go-dc"
	"github.com/direct-connect/go-dcpp/version"
)

const (
	ircDebug = false

	ircHubChan = "#hub"
)

func (h *Hub) ServeIRC(conn net.Conn) error {
	cntConnIRC.Add(1)
	cntConnIRCOpen.Add(1)
	defer cntConnIRCOpen.Add(-1)

	log.Printf("%s: using IRC", conn.RemoteAddr())
	peer, err := h.ircHandshake(conn)
	if err != nil {
		return err
	}
	defer peer.Close()

	if !h.callOnJoined(peer) {
		return nil // TODO: eny errors?
	}

	for {
		m, err := peer.readMessage()
		if err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}
		switch m.Command {
		case "PING":
			m.Command = "PONG"
			err = peer.writeMessage(m)
			if err != nil {
				return err
			}
		case "PRIVMSG":
			if len(m.Params) != 2 {
				return fmt.Errorf("invalid chat command: %#v", m)
			}
			dst, msg := m.Params[0], m.Params[1]
			if dst == ircHubChan {
				h.globalChat.SendChat(peer, Message{Text: msg})
			} else if dst := h.PeerByName(dst); dst != nil {
				h.privateChat(peer, dst, Message{
					Name: peer.Name(),
					Text: msg,
				})
			}
		case "QUIT":
			return nil
		default:
			// TODO
			log.Printf("%s: irc: %s", peer.RemoteAddr(), m)
		}
	}
}

func (h *Hub) ircHandshake(conn net.Conn) (*ircPeer, error) {
	c := irc.NewConn(conn)
	if ircDebug {
		c.Reader.DebugCallback = func(line string) { log.Println("<-", line) }
		c.Writer.DebugCallback = func(line string) { log.Println("->", line) }
	}

	host, _, _ := net.SplitHostPort(conn.LocalAddr().String())
	pref := &irc.Prefix{Name: host}

	var (
		name   string
		user   string
		unbind func()
	)
	for {
		deadline := time.Now().Add(time.Second * 5)
		_ = conn.SetReadDeadline(deadline)

		m, err := c.ReadMessage()
		if err != nil {
			return nil, fmt.Errorf("expected nick: %v", err)
		} else if m.Command != "NICK" || len(m.Params) != 1 {
			return nil, fmt.Errorf("expected nick, got: %#v", m)
		}
		tname := m.Params[0]

		if name == "" {
			// first time we expect the USER command as well
			m, err = c.ReadMessage()
			if err != nil {
				return nil, fmt.Errorf("expected user: %v", err)
			} else if m.Command != "USER" || len(m.Params) != 4 {
				return nil, fmt.Errorf("expected user, got: %#v", m)
			}

			// TODO: verify params?
			user = m.Params[0]
		}
		name = tname
		err = h.validateUserName(name)
		if err != nil {
			return nil, err
		}

		if !h.nameAvailable(name, nil) {
			_ = c.WriteMessage(&irc.Message{
				Prefix:  pref,
				Command: "433",
				Params:  []string{"*", name, errNickTaken.Error()},
			})
			continue
		}

		var ok bool
		unbind, ok = h.reserveName(name, nil, nil)
		if ok {
			break
		}
		_ = c.WriteMessage(&irc.Message{
			Prefix:  pref,
			Command: "433",
			Params:  []string{"*", name, errNickTaken.Error()},
		})
	}
	conn.SetReadDeadline(time.Time{})

	peer := &ircPeer{
		hostPref: pref,
		ownPref: &irc.Prefix{
			Name: name,
			User: user,
			Host: host,
		},
		c:    c,
		conn: conn,
	}
	h.newBasePeer(&peer.BasePeer, conn)
	peer.setName(name)

	err := h.ircAccept(peer)
	if err != nil {
		unbind()
		return nil, err
	}

	return peer, nil
}

func (h *Hub) ircAccept(peer *ircPeer) error {
	err := peer.writeMessage(&irc.Message{
		Prefix:  peer.hostPref,
		Command: "001",
		Params: []string{
			peer.Name(),
			fmt.Sprintf("Welcome to the %s Internet Relay Chat Network %s",
				h.conf.Name, peer.Name()),
		},
	})
	if err != nil {
		return err
	}
	vers := h.conf.Soft.Name + "-" + h.conf.Soft.Version

	host, port, _ := net.SplitHostPort(peer.conn.LocalAddr().String())
	err = peer.writeMessage(&irc.Message{
		Prefix:  peer.hostPref,
		Command: "002",
		Params: []string{
			peer.Name(),
			fmt.Sprintf("Your host is %s[%s/%s], running version %s",
				host, host, port, vers),
		},
	})
	if err != nil {
		return err
	}

	err = peer.writeMessage(&irc.Message{
		Prefix:  peer.hostPref,
		Command: "003",
		Params: []string{
			peer.Name(),
			fmt.Sprintf("This server was created %s at %s UTC",
				h.created.Format("Mon Jan 2 2006"), h.created.UTC().Format("15:04:05")),
		},
	})
	if err != nil {
		return err
	}

	err = peer.writeMessage(&irc.Message{
		Prefix:  peer.hostPref,
		Command: "004",
		Params: []string{
			peer.Name(),
			host,
			vers,
			// TODO: select ones that makes sense
			"DOQRSZaghilopswz", "CFILMPQSbcefgijklmnopqrstvz", "bkloveqjfI",
		},
	})
	if err != nil {
		return err
	}
	err = peer.writeMessage(&irc.Message{
		Prefix:  peer.hostPref,
		Command: "005",
		Params: []string{
			peer.Name(),
			// TODO: select ones that makes sense
			"CHANTYPES=#", "EXCEPTS", "INVEX",
			"CHANMODES=eIbq,k,flj,CFLMPQScgimnprstz",
			"CHANLIMIT=#:120", "PREFIX=(ov)@+", "MAXLIST=bqeI:100",
			"MODES=4", "NETWORK=freenode", "STATUSMSG=@+",
			"CALLERID=g", "CASEMAPPING=rfc1459",
			"are supported by this server",
		},
	})
	if err != nil {
		return err
	}

	// wait until the user joins the #hub channel
waitJoin:
	for {
		m, err := peer.readMessage()
		if err != nil {
			return err
		}
		switch m.Command {
		case "PING":
			m.Command = "PONG"
			err = peer.writeMessage(m)
			if err != nil {
				return err
			}
		case "JOIN":
			if len(m.Params) != 1 {
				return fmt.Errorf("expected the channel name, got: %#v", m)
			}
			channel := m.Params[0]
			if channel != ircHubChan {
				// TODO: write error
				return fmt.Errorf("expected the user to join %s, got: %q", ircHubChan, channel)
			}
			break waitJoin
		default:
			log.Println("unknown command:", m)
		}
	}
	err = peer.writeMessage(&irc.Message{
		Prefix:  peer.ownPref,
		Command: "JOIN",
		Params:  []string{ircHubChan},
	})
	if err != nil {
		return err
	}
	err = peer.PeersJoin(h.Peers())
	if err != nil {
		return err
	}

	var notify []Peer
	// accept the user
	h.acceptPeer(peer, nil, func() {
		notify = h.listPeers()
	})
	h.broadcastUserJoin(peer, notify)
	return nil
}

type ircPeer struct {
	BasePeer

	hostPref *irc.Prefix
	ownPref  *irc.Prefix

	conn net.Conn

	rmu sync.Mutex
	wmu sync.Mutex
	c   *irc.Conn
}

func (p *ircPeer) writeMessage(m *irc.Message) error {
	p.wmu.Lock()
	defer p.wmu.Unlock()
	return p.c.WriteMessage(m)
}

func (p *ircPeer) readMessage() (*irc.Message, error) {
	p.rmu.Lock()
	defer p.rmu.Unlock()
	return p.c.ReadMessage()
}

func (p *ircPeer) UserInfo() UserInfo {
	return UserInfo{
		Name: p.Name(),
		App: dc.Software{
			// TODO: propagate the real IRC client version
			Name:    "DC-IRC bridge",
			Version: version.Vers,
		},
	}
}

func (p *ircPeer) Close() error {
	return p.closeWith(
		p.conn.Close,
		func() error {
			p.hub.leave(p, p.sid, nil)
			return nil
		},
	)
}

func (p *ircPeer) BroadcastJoin(peers []Peer) {
	for _, p2 := range peers {
		_ = p2.PeersJoin([]Peer{p})
	}
}

func (p *ircPeer) PeersJoin(peers []Peer) error {
	for _, peer := range peers {
		m := &irc.Message{
			Command: "JOIN",
			Params:  []string{ircHubChan},
		}
		if p2, ok := peer.(*ircPeer); ok {
			m.Prefix = p2.ownPref
		} else {
			name := peer.Name()
			m.Prefix = &irc.Prefix{
				Name: name,
				User: name,
				Host: p.hostPref.Name,
			}
		}
		if err := p.writeMessage(m); err != nil {
			return err
		}
	}
	return nil
}

func (p *ircPeer) BroadcastLeave(peers []Peer) {
	for _, p2 := range peers {
		_ = p2.PeersLeave([]Peer{p})
	}
}

func (p *ircPeer) PeersLeave(peers []Peer) error {
	for _, peer := range peers {
		m := &irc.Message{
			Command: "PART",
			Params:  []string{ircHubChan, "disconnect"},
		}
		if p2, ok := peer.(*ircPeer); ok {
			m.Prefix = p2.ownPref
		} else {
			name := peer.Name()
			m.Prefix = &irc.Prefix{
				Name: name,
				User: name,
				Host: p.hostPref.Name,
			}
		}
		if err := p.writeMessage(m); err != nil {
			return err
		}
	}
	return nil
}

func (p *ircPeer) JoinRoom(room *Room) error {
	return nil // FIXME
}

func (p *ircPeer) LeaveRoom(room *Room) error {
	return nil // FIXME
}

func (p *ircPeer) ChatMsg(room *Room, from Peer, msg Message) error {
	if p == from {
		// no echo
		return nil
	}
	if room.Name() != "" {
		return nil // FIXME
	}
	m := &irc.Message{
		Command: "PRIVMSG",
		Params:  []string{ircHubChan, msg.Text},
	}
	if p2, ok := from.(*ircPeer); ok {
		m.Prefix = p2.ownPref
	} else {
		name := msg.Name
		m.Prefix = &irc.Prefix{
			Name: name,
			User: name,
			Host: p.hostPref.Name,
		}
	}
	return p.writeMessage(m)
}

func (p *ircPeer) PrivateMsg(from Peer, msg Message) error {
	m := &irc.Message{
		Command: "PRIVMSG",
		Params:  []string{p.Name(), msg.Text},
	}
	if p2, ok := from.(*ircPeer); ok {
		m.Prefix = p2.ownPref
	} else {
		name := msg.Name
		m.Prefix = &irc.Prefix{
			Name: name,
			User: name,
			Host: p.hostPref.Name,
		}
	}
	return p.writeMessage(m)
}

func (p *ircPeer) HubChatMsg(text string) error {
	// TODO:
	return nil
}

func (p *ircPeer) ConnectTo(peer Peer, addr string, token string, secure bool) error {
	// TODO: DCC?
	return nil
}

func (p *ircPeer) RevConnectTo(peer Peer, token string, secure bool) error {
	// TODO: DCC?
	return nil
}

func (p *ircPeer) Search(ctx context.Context, req SearchRequest, out Search) error {
	return nil
}

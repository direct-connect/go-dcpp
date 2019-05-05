package hub

import (
	"context"
	"net"

	dc "github.com/direct-connect/go-dc"
)

var localhostIP = net.ParseIP("127.0.0.1")

var _ Peer = (*botPeer)(nil)

type Bot struct {
	h *Hub
	p *botPeer
}

func (b *Bot) Name() string {
	return b.p.Name()
}

func (b *Bot) SendGlobal(m Message) error {
	if !b.p.Online() {
		return errConnectionClosed
	}
	m.Name = b.p.Name()
	b.h.globalChat.SendChat(b.p, m)
	return nil
}

func (b *Bot) SendPrivate(to Peer, m Message) error {
	if !b.p.Online() || !to.Online() {
		return errConnectionClosed
	}
	m.Name = b.p.Name()
	b.h.privateChat(b.p, to, m)
	return nil
}

func (b *Bot) Close() error {
	return b.p.closeWith(b.p)
}

func (h *Hub) newBot(name string, kind UserKind, soft dc.Software) (*Bot, error) {
	if err := h.validateUserName(name); err != nil {
		return nil, err
	} else if !h.nameAvailable(name, nil) {
		return nil, errNickTaken
	}
	if _, ok := h.reserveName(name, nil, nil); !ok {
		return nil, errNickTaken
	}
	if soft.Name == "" {
		soft = h.getSoft()
	}

	p := &botPeer{kind: kind, soft: soft}
	addr := &net.TCPAddr{
		IP: localhostIP,
	}
	h.newBasePeer(&p.BasePeer, &ConnInfo{
		Remote: addr,
		Local:  addr,
		Secure: true,
	})
	p.setName(name)

	var list []Peer
	h.acceptPeer(p, func() {
		list = h.listPeers()
	}, nil)
	h.broadcastUserJoin(p, list)

	b := &Bot{h: h, p: p}
	return b, nil
}

func (h *Hub) NewBot(name string, soft dc.Software) (*Bot, error) {
	return h.newBot(name, UserBot, soft)
}

type botPeer struct {
	BasePeer
	kind UserKind
	soft dc.Software
}

func (*botPeer) Searchable() bool {
	return false
}

func (p *botPeer) UserInfo() UserInfo {
	return UserInfo{
		Name: p.Name(),
		Kind: p.kind,
		App:  p.soft,
	}
}

func (p *botPeer) PeersJoin(e *PeersJoinEvent) error {
	return nil
}

func (p *botPeer) PeersUpdate(e *PeersUpdateEvent) error {
	return nil
}

func (p *botPeer) PeersLeave(e *PeersLeaveEvent) error {
	return nil
}

func (p *botPeer) PrivateMsg(from Peer, m Message) error {
	return nil
}

func (p *botPeer) HubChatMsg(m Message) error {
	return nil
}

func (p *botPeer) JoinRoom(room *Room) error {
	return nil
}

func (p *botPeer) ChatMsg(room *Room, from Peer, m Message) error {
	return nil
}

func (p *botPeer) LeaveRoom(room *Room) error {
	return nil
}

func (p *botPeer) ConnectTo(peer Peer, addr string, token string, secure bool) error {
	return nil
}

func (p *botPeer) RevConnectTo(peer Peer, token string, secure bool) error {
	return nil
}

func (p *botPeer) Search(ctx context.Context, req SearchRequest, out Search) error {
	_ = out.Close()
	return nil
}

func (p *botPeer) Close() error {
	return nil // override - can only be closed by bot owner
}

package hub

import (
	"context"
	"net"

	"github.com/direct-connect/go-dc/types"
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

func (b *Bot) UserInfo() UserInfo {
	return b.p.UserInfo()
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

func (h *Hub) newBot(name, desc, email string, kind UserKind, soft types.Software) (*Bot, error) {
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

	p := &botPeer{kind: kind, soft: soft, desc: desc, email: email}
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

func (h *Hub) NewBot(name string, soft types.Software) (*Bot, error) {
	return h.NewBotDesc(name, "", "", soft)
}

func (h *Hub) NewBotDesc(name, desc, email string, soft types.Software) (*Bot, error) {
	return h.newBot(name, desc, email, UserBot, soft)
}

type botPeer struct {
	BasePeer
	desc  string
	email string
	kind  UserKind
	soft  types.Software
}

func (*botPeer) Searchable() bool {
	return false
}

func (p *botPeer) UserInfo() UserInfo {
	return UserInfo{
		Name:           p.Name(),
		Kind:           p.kind,
		App:            p.soft,
		Desc:           p.desc,
		Email:          p.email,
		HubsNormal:     1,
		HubsRegistered: 1,
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

func (p *botPeer) Redirect(addr string) error {
	return nil
}

func (p *botPeer) Close() error {
	return nil // override - can only be closed by bot owner
}

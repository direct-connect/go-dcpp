package hub

import (
	"context"
	"net"
	"sync"

	"github.com/direct-connect/go-dcpp/internal/safe"
)

type Peer interface {
	base() *BasePeer

	Online() bool
	SID() SID
	Name() string
	LocalAddr() net.Addr
	RemoteAddr() net.Addr
	User() User

	Close() error

	PeersJoin(peers []Peer) error
	PeersLeave(peers []Peer) error
	BroadcastJoin(peers []Peer)
	BroadcastLeave(peers []Peer)
	//PeersUpdate(peers []Peer) error

	PrivateMsg(from Peer, m Message) error
	HubChatMsg(text string) error

	JoinRoom(room *Room) error
	ChatMsg(room *Room, from Peer, m Message) error
	LeaveRoom(room *Room) error

	ConnectTo(peer Peer, addr string, token string, secure bool) error
	RevConnectTo(peer Peer, token string, secure bool) error

	Search(ctx context.Context, req SearchRequest, out Search) error
}

type BasePeer struct {
	hub     *Hub
	offline safe.Bool

	hubAddr  net.Addr
	peerAddr net.Addr
	sid      SID
	name     safe.String

	rooms struct {
		sync.RWMutex
		list []*Room
	}
}

func (p *BasePeer) base() *BasePeer {
	return p
}

func (p *BasePeer) setOffline() {
	p.offline.Set(true)
}

func (p *BasePeer) setName(name string) {
	p.name.Set(name)
}

func (p *BasePeer) Name() string {
	return p.name.Get()
}

func (p *BasePeer) Online() bool {
	return !p.offline.Get()
}

func (p *BasePeer) SID() SID {
	return p.sid
}

func (p *BasePeer) LocalAddr() net.Addr {
	return p.hubAddr
}

func (p *BasePeer) RemoteAddr() net.Addr {
	return p.peerAddr
}

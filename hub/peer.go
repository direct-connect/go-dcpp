package hub

import (
	"context"
	"net"
	"sync"

	"github.com/direct-connect/go-dcpp/internal/safe"
)

type connAddr interface {
	LocalAddr() net.Addr
	RemoteAddr() net.Addr
}

type Peer interface {
	base() *BasePeer

	Online() bool
	SID() SID
	Name() string
	connAddr
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

func (h *Hub) newBasePeer(p *BasePeer, c connAddr) {
	*p = BasePeer{
		hub:      h,
		hubAddr:  c.LocalAddr(),
		peerAddr: c.RemoteAddr(),
		sid:      h.nextSID(),
	}
	p.close.done = make(chan struct{})
}

type BasePeer struct {
	hub     *Hub
	offline safe.Bool

	hubAddr  net.Addr
	peerAddr net.Addr
	sid      SID
	name     safe.String

	close struct {
		sync.Mutex
		done chan struct{}
	}

	rooms struct {
		sync.RWMutex
		list []*Room
	}
}

func (p *BasePeer) base() *BasePeer {
	return p
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

func (p *BasePeer) closeWith(closers ...func() error) error {
	if !p.Online() {
		return nil
	}
	p.close.Lock()
	defer p.close.Unlock()
	if !p.Online() {
		return nil
	}
	close(p.close.done)
	p.offline.Set(true)
	var first error
	for _, fnc := range closers {
		if err := fnc(); err != nil {
			first = err
		}
	}
	return first
}

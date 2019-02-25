package hub

import (
	"context"
	"net"
	"sync"
)

type Peer interface {
	base() *BasePeer

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

	Search(ctx context.Context, req SearchReq, out Search) error
	SearchTTH(ctx context.Context, tth TTH, out Search) error
}

type BasePeer struct {
	hub *Hub

	hubAddr  net.Addr
	peerAddr net.Addr
	sid      SID

	rooms struct {
		sync.RWMutex
		list []*Room
	}
}

func (p *BasePeer) base() *BasePeer {
	return p
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

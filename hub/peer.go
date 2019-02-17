package hub

import (
	"net"
)

type Peer interface {
	SID() SID
	Name() string
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
}

type BasePeer struct {
	hub *Hub

	addr net.Addr
	sid  SID
}

func (p *BasePeer) SID() SID {
	return p.sid
}

func (p *BasePeer) RemoteAddr() net.Addr {
	return p.addr
}

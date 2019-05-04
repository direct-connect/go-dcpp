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

type ConnInfo struct {
	Local   net.Addr
	Remote  net.Addr
	Secure  bool
	TLSVers uint16
	ALPN    string
}

type Peer interface {
	base() *BasePeer
	setUser(u *User)
	connAddr

	// Close the peer's connection.
	Close() error

	// ConnInfo returns a connection information for this peer.
	// The returned value should not be changed.
	ConnInfo() *ConnInfo

	// User returns a user associated with this peer. It may be nil.
	User() *User

	// Online flag for this peer.
	Online() bool
	// Searchable checks if this peer accepts search requests.
	Searchable() bool

	// SID returns a session ID of this peer.
	SID() SID

	// Name returns peer's user name.
	Name() string

	// UserInfo returns a snapshot of a user info.
	UserInfo() UserInfo

	// PeersJoin sends a set of "peer join" events to this peer.
	PeersJoin(e *PeersJoinEvent) error
	// PeersUpdate sends a set of "peer info update" events to this peer.
	PeersUpdate(e *PeersUpdateEvent) error
	// PeersLeave sends a set of "peer leave" events to this peer.
	PeersLeave(e *PeersLeaveEvent) error

	// PrivateMsg sends a private message for this peer.
	PrivateMsg(from Peer, m Message) error
	// HubChatMsg sends a global message from the hub.
	HubChatMsg(m Message) error

	// JoinRoom sends a "room join" event for this peer.
	JoinRoom(room *Room) error
	// ChatMsg sends a chat message from a specific room to this peer.
	ChatMsg(room *Room, from Peer, m Message) error
	// LeaveRoom sends a "room leave" event for this peer.
	LeaveRoom(room *Room) error

	// ConnectTo sends a connection request to this peer.
	ConnectTo(peer Peer, addr string, token string, secure bool) error
	// RevConnectTo sends a reverse connection request to this peer.
	RevConnectTo(peer Peer, token string, secure bool) error

	// Search sends a search request to this peer.
	Search(ctx context.Context, req SearchRequest, out Search) error
}

type PeersJoinEvent struct {
	Peers []Peer

	nmdcInfos nmdcRaw
	nmdcOps   nmdcRaw
	nmdcBots  nmdcRaw
	nmdcIPs   nmdcRaw
}

type PeersUpdateEvent struct {
	Peers []Peer

	nmdcInfos nmdcRaw
	nmdcOps   nmdcRaw
	nmdcBots  nmdcRaw
	nmdcIPs   nmdcRaw
}

type PeersLeaveEvent struct {
	Peers []Peer

	nmdcQuit nmdcRaw
}

func (h *Hub) newBasePeer(p *BasePeer, c *ConnInfo) {
	*p = BasePeer{
		hub:   h,
		cinfo: c,
		sid:   h.nextSID(),
	}
	p.close.done = make(chan struct{})
}

type BasePeer struct {
	hub     *Hub
	cinfo   *ConnInfo
	user    *User
	offline safe.Bool

	sid  SID
	name safe.String

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

func (p *BasePeer) setUser(u *User) {
	p.user = u
}

func (p *BasePeer) User() *User {
	return p.user
}

func (p *BasePeer) ConnInfo() *ConnInfo {
	return p.cinfo
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
	return p.cinfo.Local
}

func (p *BasePeer) RemoteAddr() net.Addr {
	return p.cinfo.Remote
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

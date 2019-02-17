package hub

import (
	"net"

	"github.com/direct-connect/go-dcpp/adc"
)

type Peer interface {
	SID() adc.SID
	Name() string
	RemoteAddr() net.Addr
	User() User

	Close() error

	PeersJoin(peers []Peer) error
	PeersLeave(peers []Peer) error
	BroadcastJoin(peers []Peer)
	BroadcastLeave(peers []Peer)
	//PeersUpdate(peers []Peer) error

	ChatMsg(from Peer, m Message) error
	PrivateMsg(from Peer, m Message) error
	HubChatMsg(text string) error

	ConnectTo(peer Peer, addr string, token string, secure bool) error
	RevConnectTo(peer Peer, token string, secure bool) error
}

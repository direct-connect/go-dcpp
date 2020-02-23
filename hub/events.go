package hub

import (
	"net"
	"sync"
)

type hooks struct {
	sync.RWMutex

	// connection events
	onConnected    []func(c net.Conn) bool
	onConnectedIP4 []func(c net.Conn, ip net.IP) bool
	onConnectedIP6 []func(c net.Conn, ip net.IP) bool
	onDisconnected []func(c net.Conn)

	// peer state events
	onJoined []func(p Peer) bool
	onLeave  []func(p Peer)

	// chat events
	onGlobalChat []func(p Peer, m Message) bool
	onChat       []func(r *Room, p Peer, m Message) bool
	onPM         []func(from, to Peer, m Message) bool
}

// OnConnected registers a trigger for a moment when a new connection is accepted, but before the protocol detection.
// The function returns a flag indicating if a connection should be accepted or not.
func (h *Hub) OnConnected(fnc func(c net.Conn) bool) {
	h.hooks.Lock()
	h.hooks.onConnected = append(h.hooks.onConnected, fnc)
	h.hooks.Unlock()
}

// OnConnectedIP4 registers a trigger for a moment when a new connection from IPv4 is accepted, but before the protocol detection.
// The function returns a flag indicating if a connection should be accepted or not.
func (h *Hub) OnConnectedIP4(fnc func(c net.Conn, ip net.IP) bool) {
	h.hooks.Lock()
	h.hooks.onConnectedIP4 = append(h.hooks.onConnectedIP4, fnc)
	h.hooks.Unlock()
}

// OnConnectedIP6 registers a trigger for a moment when a new connection from IPv6 is accepted, but before the protocol detection.
// The function returns a flag indicating if a connection should be accepted or not.
func (h *Hub) OnConnectedIP6(fnc func(c net.Conn, ip net.IP) bool) {
	h.hooks.Lock()
	h.hooks.onConnectedIP6 = append(h.hooks.onConnectedIP6, fnc)
	h.hooks.Unlock()
}

// OnDisconnected registers a trigger for a moment when the connection is closed.
func (h *Hub) OnDisconnected(fnc func(c net.Conn)) {
	h.hooks.Lock()
	h.hooks.onDisconnected = append(h.hooks.onDisconnected, fnc)
	h.hooks.Unlock()
}

// OnJoined is triggered when the peer successfully joins the hub, after all the checks and right before the client event loop.
// The function returns a flag if a peer should be accepted or not.
func (h *Hub) OnJoined(fnc func(p Peer) bool) {
	h.hooks.Lock()
	h.hooks.onJoined = append(h.hooks.onJoined, fnc)
	h.hooks.Unlock()
}

// OnPermJoined is an analog of OnJoined, but triggers only when a user with a specified permission joins.
func (h *Hub) OnPermJoined(perm string, fnc func(p Peer) bool) {
	h.OnJoined(func(p Peer) bool {
		if !p.User().HasPerm(perm) {
			return true
		}
		return fnc(p)
	})
}

// OnLeave is triggered when the peer disconnects from the hub, after he was unregistered from the users list.
func (h *Hub) OnLeave(fnc func(p Peer)) {
	h.hooks.Lock()
	h.hooks.onLeave = append(h.hooks.onLeave, fnc)
	h.hooks.Unlock()
}

// OnPermLeave is an analog of OnLeave, but triggers only when a user with a specified permission leaves.
func (h *Hub) OnPermLeave(perm string, fnc func(p Peer)) {
	h.OnLeave(func(p Peer) {
		if p.User().HasPerm(perm) {
			fnc(p)
		}
	})
}

// OnGlobalChat is triggered when the message is sent in the global chat.
// The function returns a flag if a message should be accepted or not.
func (h *Hub) OnGlobalChat(fnc func(p Peer, m Message) bool) {
	h.hooks.Lock()
	h.hooks.onGlobalChat = append(h.hooks.onGlobalChat, fnc)
	h.hooks.Unlock()
}

// OnChat is triggered when the message is sent in any of the chat rooms.
// The function returns a flag if a message should be accepted or not.
func (h *Hub) OnChat(fnc func(r *Room, p Peer, m Message) bool) {
	h.hooks.Lock()
	h.hooks.onChat = append(h.hooks.onChat, fnc)
	h.hooks.Unlock()
}

// OnPM is triggered when the private message is sent through the hub.
// The function returns a flag if a message should be accepted or not.
func (h *Hub) OnPM(fnc func(from, to Peer, m Message) bool) {
	h.hooks.Lock()
	h.hooks.onPM = append(h.hooks.onPM, fnc)
	h.hooks.Unlock()
}

func (h *Hub) callOnConnected(c net.Conn) bool {
	h.hooks.RLock()
	defer h.hooks.RUnlock()
	for _, fnc := range h.hooks.onConnected {
		if !fnc(c) {
			return false
		}
	}
	if len(h.hooks.onConnectedIP4)+len(h.hooks.onConnectedIP6) == 0 {
		return true
	}
	if addr, ok := c.RemoteAddr().(*net.TCPAddr); ok {
		if len(addr.IP) == net.IPv6len {
			ip6 := addr.IP.To16()
			for _, fnc := range h.hooks.onConnectedIP6 {
				if !fnc(c, ip6) {
					return false
				}
			}
		}
		if ip4 := addr.IP.To4(); ip4 != nil {
			for _, fnc := range h.hooks.onConnectedIP4 {
				if !fnc(c, ip4) {
					return false
				}
			}
		}
	}
	return true
}

func (h *Hub) callOnDisconnected(c net.Conn) {
	h.hooks.RLock()
	defer h.hooks.RUnlock()
	for _, fnc := range h.hooks.onDisconnected {
		fnc(c)
	}
}

func (h *Hub) callOnJoined(p Peer) bool {
	h.hooks.RLock()
	defer h.hooks.RUnlock()
	for _, fnc := range h.hooks.onJoined {
		if !fnc(p) {
			return false
		}
	}
	return true
}

func (h *Hub) callOnLeave(p Peer) {
	h.hooks.RLock()
	defer h.hooks.RUnlock()
	for _, fnc := range h.hooks.onLeave {
		fnc(p)
	}
}

func (h *Hub) callOnGlobalChat(p Peer, m Message) bool {
	h.hooks.RLock()
	defer h.hooks.RUnlock()
	for _, fnc := range h.hooks.onGlobalChat {
		if !fnc(p, m) {
			return false
		}
	}
	return true
}

func (h *Hub) callOnChat(r *Room, p Peer, m Message) bool {
	h.hooks.RLock()
	defer h.hooks.RUnlock()
	for _, fnc := range h.hooks.onChat {
		if !fnc(r, p, m) {
			return false
		}
	}
	return true
}

func (h *Hub) callOnPM(from, to Peer, m Message) bool {
	h.hooks.RLock()
	defer h.hooks.RUnlock()
	for _, fnc := range h.hooks.onPM {
		if !fnc(from, to, m) {
			return false
		}
	}
	return true
}

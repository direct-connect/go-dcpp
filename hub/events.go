package hub

import (
	"net"
	"sync"
)

type hooks struct {
	sync.RWMutex
	onConnected    []func(c net.Conn) bool
	onDisconnected []func(c net.Conn)
	onJoined       []func(p Peer) bool
	onLeave        []func(p Peer)
	onChat         []func(p Peer, m Message) bool
}

func (h *Hub) OnConnected(fnc func(c net.Conn) bool) {
	h.hooks.Lock()
	h.hooks.onConnected = append(h.hooks.onConnected, fnc)
	h.hooks.Unlock()
}

func (h *Hub) OnDisconnected(fnc func(c net.Conn)) {
	h.hooks.Lock()
	h.hooks.onDisconnected = append(h.hooks.onDisconnected, fnc)
	h.hooks.Unlock()
}

func (h *Hub) OnJoined(fnc func(p Peer) bool) {
	h.hooks.Lock()
	h.hooks.onJoined = append(h.hooks.onJoined, fnc)
	h.hooks.Unlock()
}

func (h *Hub) OnLeave(fnc func(p Peer)) {
	h.hooks.Lock()
	h.hooks.onLeave = append(h.hooks.onLeave, fnc)
	h.hooks.Unlock()
}

func (h *Hub) OnChat(fnc func(p Peer, m Message) bool) {
	h.hooks.Lock()
	h.hooks.onChat = append(h.hooks.onChat, fnc)
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

func (h *Hub) callOnChat(p Peer, m Message) bool {
	h.hooks.RLock()
	defer h.hooks.RUnlock()
	for _, fnc := range h.hooks.onChat {
		if !fnc(p, m) {
			return false
		}
	}
	return true
}

package hub

import (
	"net"
	"sync"
)

type events struct {
	sync.RWMutex
	onConnected    []func(c net.Conn) bool
	onDisconnected []func(c net.Conn)
	onJoined       []func(p Peer) bool
	onChat         []func(p Peer, m Message) bool
}

func (h *Hub) OnConnected(fnc func(c net.Conn) bool) {
	h.events.Lock()
	h.events.onConnected = append(h.events.onConnected, fnc)
	h.events.Unlock()
}

func (h *Hub) OnDisconnected(fnc func(c net.Conn)) {
	h.events.Lock()
	h.events.onDisconnected = append(h.events.onDisconnected, fnc)
	h.events.Unlock()
}

func (h *Hub) OnJoined(fnc func(p Peer) bool) {
	h.events.Lock()
	h.events.onJoined = append(h.events.onJoined, fnc)
	h.events.Unlock()
}

func (h *Hub) OnChat(fnc func(p Peer, m Message) bool) {
	h.events.Lock()
	h.events.onChat = append(h.events.onChat, fnc)
	h.events.Unlock()
}

func (h *Hub) callOnConnected(c net.Conn) bool {
	h.events.RLock()
	defer h.events.RUnlock()
	for _, fnc := range h.events.onConnected {
		if !fnc(c) {
			return false
		}
	}
	return true
}

func (h *Hub) callOnDisconnected(c net.Conn) {
	h.events.RLock()
	defer h.events.RUnlock()
	for _, fnc := range h.events.onDisconnected {
		fnc(c)
	}
}

func (h *Hub) callOnJoined(p Peer) bool {
	h.events.RLock()
	defer h.events.RUnlock()
	for _, fnc := range h.events.onJoined {
		if !fnc(p) {
			return false
		}
	}
	return true
}

func (h *Hub) callOnChat(p Peer, m Message) bool {
	h.events.RLock()
	defer h.events.RUnlock()
	for _, fnc := range h.events.onChat {
		if !fnc(p, m) {
			return false
		}
	}
	return true
}

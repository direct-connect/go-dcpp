package hub

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

var (
	ErrRoomExists = errors.New("room already exists")
)

type Message struct {
	Time time.Time
	Name string
	Text string
}

func (h *Hub) newRoom(name string) *Room {
	cntChatRooms.Add(1)
	r := &Room{
		h: h, name: name, sid: h.nextSID(),
		peers: make(map[Peer]struct{}),
	}
	r.log.limit = h.conf.ChatLog
	return r
}

func (h *Hub) NewRoom(name string) (*Room, error) {
	if !strings.HasPrefix(name, "#") {
		return nil, errors.New("room name should start with '#'")
	}
	h.rooms.RLock()
	_, ok := h.rooms.byName[name]
	h.rooms.RUnlock()
	if ok {
		return nil, ErrRoomExists
	}
	h.rooms.Lock()
	_, ok = h.rooms.byName[name]
	if ok {
		h.rooms.Unlock()
		return nil, ErrRoomExists
	}
	r := h.newRoom(name)
	h.rooms.byName[name] = r
	h.rooms.bySID[r.sid] = r
	h.rooms.Unlock()
	log.Printf("new room: %q", name)
	return r, nil
}

func (h *Hub) Room(name string) *Room {
	h.rooms.RLock()
	r := h.rooms.byName[name]
	h.rooms.RUnlock()
	return r
}

func (h *Hub) roomBySID(sid SID) *Room {
	h.rooms.RLock()
	r := h.rooms.bySID[sid]
	h.rooms.RUnlock()
	return r
}

type Room struct {
	h    *Hub
	name string
	sid  SID

	lmu sync.RWMutex
	log chatBuffer

	pmu   sync.RWMutex
	peers map[Peer]struct{}
}

func (r *Room) SID() SID {
	return r.sid
}

func (r *Room) Name() string {
	return r.name
}

func (r *Room) Users() int {
	r.pmu.RLock()
	n := len(r.peers)
	r.pmu.RUnlock()
	return n
}

func (r *Room) InRoom(p Peer) bool {
	r.pmu.RLock()
	_, ok := r.peers[p]
	r.pmu.RUnlock()
	return ok
}

func (r *Room) Peers() []Peer {
	r.pmu.RLock()
	list := make([]Peer, 0, len(r.peers))
	for p := range r.peers {
		list = append(list, p)
	}
	r.pmu.RUnlock()
	return list
}

func (r *Room) Join(p Peer) {
	r.pmu.Lock()
	_, ok := r.peers[p]
	if !ok {
		r.peers[p] = struct{}{}
		pb := p.base()
		pb.rooms.Lock()
		pb.rooms.list = append(pb.rooms.list, r)
		pb.rooms.Unlock()
	}
	r.pmu.Unlock()
	if !ok {
		_ = p.JoinRoom(r)
	}
}

func (r *Room) Leave(p Peer) {
	r.pmu.Lock()
	_, ok := r.peers[p]
	if ok {
		delete(r.peers, p)
	}
	r.pmu.Unlock()
	if ok {
		_ = p.LeaveRoom(r)
	}
}

func (r *Room) SendChat(from Peer, text string) {
	m := Message{
		Time: time.Now().UTC(),
		Name: from.Name(),
		Text: text,
	}

	if r.h.globalChat == r {
		if !r.h.callOnChat(from, m) {
			cntChatMsgDropped.Add(1)
			return
		}
	}

	cntChatMsg.Add(1)

	if r.h.conf.ChatLog > 0 {
		r.lmu.Lock()
		r.log.Append(m)
		r.lmu.Unlock()
	}

	for _, p := range r.Peers() {
		_ = p.ChatMsg(r, from, m)
	}
}

func (r *Room) ReplayChat(to Peer, n int) {
	if r.h.conf.ChatLog <= 0 {
		return
	}

	r.lmu.RLock()
	log := r.log.Get(n)
	r.lmu.RUnlock()

	for _, m := range log {
		// TODO: replay messages from peers themselves, if they are still online
		err := to.HubChatMsg(fmt.Sprintf(
			"[%s] <%s> %s",
			m.Time.Format("15:04:05"),
			m.Name, m.Text,
		))
		if err != nil {
			return
		}
	}
}

type chatBuffer struct {
	start int
	limit int
	ring  []Message
}

func (c *chatBuffer) Get(n int) []Message {
	if n <= 0 || n > len(c.ring) {
		n = len(c.ring)
	}
	out := make([]Message, n)
	// index in the dst array where new messages (head) start
	i := n - c.start
	// index in the src (head) for an amount of messages that fit into dst
	j := 0
	if i < 0 {
		j = -i
		i = 0
	}
	copy(out[i:], c.ring[j:c.start])
	if i == 0 {
		return out
	}
	// index in the src (tail) for an amount of messages that fit into dst
	j = len(c.ring) - i
	copy(out[:i], c.ring[j:j+i])
	return out
}

func (c *chatBuffer) Append(m Message) {
	if len(c.ring) < c.limit {
		c.ring = append(c.ring, m)
		return
	}
	// ring buffer - overwrite an oldest item and advance the pointer
	c.ring[c.start] = m
	c.start++
	if c.start >= len(c.ring) {
		c.start = 0
	}
}

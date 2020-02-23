package hub

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	ErrRoomExists   = errors.New("room already exists")
	ErrCantJoinRoom = errors.New("cannot join the room")
)

type Message struct {
	Time time.Time
	Name string
	Text string
	Me   bool
}

type rooms struct {
	sync.RWMutex
	byName map[nameKey]*Room
	bySID  map[SID]*Room
}

func roomKey(name string) nameKey {
	if name == "" {
		return ""
	}
	if name[0] == '#' {
		name = name[1:]
	}
	name = strings.ToLower(name)
	return nameKey(name)
}

func (r *rooms) init() {
	r.byName = make(map[nameKey]*Room)
	r.bySID = make(map[SID]*Room)
}

func (h *Hub) newRoomSys(name string, perm string) *Room {
	cntChatRooms.Add(1)
	if perm != "" {
		cntChatRoomsPrivate.Add(1)
	}
	r := &Room{
		h: h, name: name, sid: h.nextSID(),
		perm:  perm,
		peers: make(map[Peer]struct{}),
	}
	r.log.limit = h.conf.ChatLog
	return r
}

func (h *Hub) newRoom(name string, perm string) (*Room, error) {
	key := roomKey(name)
	if len(name) != 0 && name[0] != '#' {
		name = "#" + name
	}
	h.rooms.RLock()
	_, ok := h.rooms.byName[key]
	h.rooms.RUnlock()
	if ok {
		return nil, ErrRoomExists
	}
	h.rooms.Lock()
	_, ok = h.rooms.byName[key]
	if ok {
		h.rooms.Unlock()
		return nil, ErrRoomExists
	}
	r := h.newRoomSys(name, perm)
	r.perm = perm
	h.rooms.byName[key] = r
	h.rooms.bySID[r.sid] = r
	h.rooms.Unlock()
	h.Logf("new room: %q", name)
	return r, nil
}

// NewRoom creates a new public room with a given name.
func (h *Hub) NewRoom(name string) (*Room, error) {
	return h.newRoom(name, "")
}

// NewPrivateRoom creates a new private room. It cannot be entered by anyone except the owner.
// It's the caller's responsibility to add users to this kind of room.
func (h *Hub) NewPrivateRoom(name string) (*Room, error) {
	return h.newRoom(name, "-")
}

// NewPermRoom creates a new private room that can be accessed by anyone with a given permission.
func (h *Hub) NewPermRoom(name string, perm string) (*Room, error) {
	return h.newRoom(name, perm)
}

// Room finds a room with a given name. It returns nil if room doesn't exist.
// This function won't be able to retrieve the global chat. See GlobalChatRoom for this.
func (h *Hub) Room(name string) *Room {
	if len(name) == 0 {
		return nil
	}
	key := roomKey(name)
	h.rooms.RLock()
	r := h.rooms.byName[key]
	h.rooms.RUnlock()
	return r
}

// GlobalChatRoom returns a global chat room that all users join by default when entering the hub.
func (h *Hub) GlobalChatRoom() *Room {
	return h.globalChat
}

// Rooms returns a list of all rooms that exist on the hub. It won't list the global chat.
// This function returns all rooms regardless of user's permissions. Use RoomsFor to get a filtered list.
func (h *Hub) Rooms() []*Room {
	h.rooms.RLock()
	defer h.rooms.RUnlock()
	list := make([]*Room, 0, len(h.rooms.byName))
	for _, r := range h.rooms.byName {
		list = append(list, r)
	}
	return list
}

// RoomsFor returns a list of all rooms that are accessible by the peer. It won't list the global chat.
func (h *Hub) RoomsFor(p Peer) []*Room {
	rooms := h.Rooms()
	var out []*Room
	for _, r := range rooms {
		if r.CanJoin(p) {
			out = append(out, r)
		}
	}
	return out
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
	perm string

	lmu sync.RWMutex
	log chatBuffer

	pmu   sync.RWMutex
	peers map[Peer]struct{}
}

func (r *Room) SID() SID {
	return r.sid
}

// Name returns a room name.
func (r *Room) Name() string {
	return r.name
}

// IsPrivate reports if a room is private.
func (r *Room) IsPrivate() bool {
	return r.perm != ""
}

// Users returns the number of users currently in the room.
func (r *Room) Users() int {
	r.pmu.RLock()
	n := len(r.peers)
	r.pmu.RUnlock()
	return n
}

// InRoom checks if the peer is already in this room.
func (r *Room) InRoom(p Peer) bool {
	r.pmu.RLock()
	_, ok := r.peers[p]
	r.pmu.RUnlock()
	return ok
}

// CanJoin checks if the peer can access the room.
func (r *Room) CanJoin(p Peer) bool {
	if !r.IsPrivate() {
		return true
	}
	u := p.User()
	if u.IsOwner() {
		return true
	}
	if r.perm != "" && u.HasPerm(r.perm) {
		return true
	}
	// TODO(dennwc): invites
	return r.InRoom(p)
}

// Peers returns a list of peers currently in the room.
func (r *Room) Peers() []Peer {
	r.pmu.RLock()
	list := make([]Peer, 0, len(r.peers))
	for p := range r.peers {
		list = append(list, p)
	}
	r.pmu.RUnlock()
	return list
}

// Join adds the peer to the room. It will not run any additional checks. See CanJoin for this.
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

// Leave removes the peer from the room.
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

// SendChat sends chat message to the room.
func (r *Room) SendChat(from Peer, m Message) {
	m.Time = time.Now().UTC()
	if m.Name == "" {
		m.Name = from.Name()
	}

	if r.h.globalChat == r {
		if !r.h.callOnGlobalChat(from, m) {
			cntChatMsgDropped.Add(1)
			return
		}
	}
	if !r.h.callOnChat(r, from, m) {
		cntChatMsgDropped.Add(1)
		return
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

// ReplayChat replays the chat log from this room to a given peer.
func (r *Room) ReplayChat(to Peer, n int) {
	if r.h.conf.ChatLog <= 0 {
		return
	}

	r.lmu.RLock()
	log := r.log.Get(n)
	r.lmu.RUnlock()

	for _, m := range log {
		// TODO: replay messages from peers themselves, if they are still online
		var txt string
		if m.Me {
			txt = fmt.Sprintf(
				"[%s] * %s %s",
				m.Time.Format("15:04:05"),
				m.Name, m.Text,
			)
		} else {
			txt = fmt.Sprintf(
				"[%s] <%s> %s",
				m.Time.Format("15:04:05"),
				m.Name, m.Text,
			)
		}
		err := to.HubChatMsg(Message{Text: txt, Time: m.Time})
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

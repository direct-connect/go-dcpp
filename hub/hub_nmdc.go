package hub

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/text/encoding"

	"github.com/direct-connect/go-dcpp/nmdc"
)

const nmdcFakeToken = "nmdc"

func (h *Hub) ServeNMDC(conn net.Conn) error {
	log.Printf("%s: using NMDC", conn.RemoteAddr())
	c, err := nmdc.NewConn(conn)
	if err != nil {
		return err
	}
	defer c.Close()
	c.SetFallbackEncoding(h.fallback)

	peer, err := h.nmdcHandshake(c)
	if err != nil {
		return err
	} else if peer == nil {
		return nil // pingers
	}
	defer peer.Close()
	return h.nmdcServePeer(peer)
}

func (h *Hub) nmdcLock(deadline time.Time, c *nmdc.Conn) (nmdc.Features, nmdc.Name, error) {
	lock := &nmdc.Lock{
		Lock: "EXTENDEDPROTOCOL_godcpp", // TODO: randomize
		PK:   h.conf.Soft.Name + " " + h.conf.Soft.Vers,
	}
	err := c.WriteMsg(lock)
	if err != nil {
		return nil, "", err
	}
	err = c.Flush()
	if err != nil {
		return nil, "", err
	}

	var sup nmdc.Supports
	err = c.ReadMsgTo(deadline, &sup)
	if err != nil {
		return nil, "", fmt.Errorf("expected supports: %v", err)
	}
	var key nmdc.Key
	err = c.ReadMsgTo(deadline, &key)
	if err != nil {
		return nil, "", fmt.Errorf("expected key: %v", err)
	} else if key.Key != lock.Key().Key {
		return nil, "", errors.New("wrong key")
	}
	fea := make(nmdc.Features, len(sup.Ext))
	for _, f := range sup.Ext {
		fea[f] = struct{}{}
	}
	if !fea.Has(nmdc.FeaNoHello) {
		return nil, "", errors.New("NoHello is not supported")
	} else if !fea.Has(nmdc.FeaNoGetINFO) {
		return nil, "", errors.New("NoGetINFO is not supported")
	}
	nick, err := c.ReadValidateNick(deadline)
	if err != nil {
		return nil, "", err
	}
	return fea, nick.Name, nil
}

var nmdcFeatures = nmdc.Features{
	nmdc.FeaNoHello:     {},
	nmdc.FeaNoGetINFO:   {},
	nmdc.FeaBotINFO:     {},
	nmdc.FeaTTHSearch:   {},
	nmdc.FeaUserIP2:     {},
	nmdc.FeaUserCommand: {},
}

func (h *Hub) nmdcHandshake(c *nmdc.Conn) (*nmdcPeer, error) {
	deadline := time.Now().Add(time.Second * 5)

	fea, nick, err := h.nmdcLock(deadline, c)
	if err != nil {
		_ = c.WriteMsg(&nmdc.ChatMessage{Text: err.Error()})
		_ = c.Flush()
		return nil, err
	}
	addr, ok := c.RemoteAddr().(*net.TCPAddr)
	if !ok {
		err = fmt.Errorf("not a tcp address: %T", c.RemoteAddr())
		_ = c.WriteMsg(&nmdc.ChatMessage{Text: err.Error()})
		_ = c.Flush()
		return nil, err
	}
	name := string(nick)
	err = h.validateUserName(name)
	if err != nil {
		_ = c.WriteMsg(&nmdc.ChatMessage{Text: err.Error()})
		_ = c.Flush()
		return nil, err
	}

	peer := &nmdcPeer{
		BasePeer: BasePeer{
			hub:      h,
			hubAddr:  c.LocalAddr(),
			peerAddr: c.RemoteAddr(),
			sid:      h.nextSID(),
		},
		conn: c, ip: addr.IP,
		fea: nmdcFeatures.Intersect(fea),
	}
	peer.user.Name = nick

	if peer.fea.Has(nmdc.FeaBotINFO) {
		// it's a pinger - don't bother binding the nickname
		delete(peer.fea, nmdc.FeaBotINFO)
		peer.fea.Set(nmdc.FeaHubINFO)

		err = h.nmdcAccept(peer)
		if err != nil {
			return nil, err
		}
		var bot nmdc.BotINFO
		if err := c.ReadMsgTo(deadline, &bot); err != nil {
			return nil, err
		}
		st := h.Stats()
		err = c.WriteMsg(&nmdc.HubINFO{
			Name:     nmdc.Name(st.Name),
			Desc:     nmdc.String(st.Desc),
			Host:     st.DefaultAddr(),
			Soft:     st.Soft.Name + " " + st.Soft.Vers,
			Encoding: "UTF8",
		})
		if err == nil {
			err = c.Flush()
		}
		return nil, err
	}

	// do not lock for writes first
	h.peers.RLock()
	_, sameName1 := h.peers.reserved[name]
	_, sameName2 := h.peers.byName[name]
	h.peers.RUnlock()

	if sameName1 || sameName2 {
		_ = peer.writeOneNow(&nmdc.ValidateDenide{nick})
		return nil, errNickTaken
	}

	// ok, now lock for writes and try to bind nick
	h.peers.Lock()
	_, sameName1 = h.peers.reserved[name]
	_, sameName2 = h.peers.byName[name]
	if sameName1 || sameName2 {
		h.peers.Unlock()

		_ = peer.writeOneNow(&nmdc.ValidateDenide{nick})
		return nil, errNickTaken
	}
	// bind nick, still no one will see us yet
	h.peers.reserved[name] = struct{}{}
	h.peers.Unlock()

	err = h.nmdcAccept(peer)
	if err != nil || peer.getState() == nmdcPeerClosed {
		h.peers.Lock()
		delete(h.peers.reserved, name)
		h.peers.Unlock()

		str := "connection is closed"
		if err != nil {
			str = err.Error()
		}
		_ = peer.writeOneNow(&nmdc.ChatMessage{Text: "handshake failed: " + str})
		return nil, err
	}

	// finally accept the user on the hub
	h.peers.Lock()
	// cleanup temporary bindings
	delete(h.peers.reserved, name)

	// make a snapshot of peers to send info to
	list := h.listPeers()

	// add user to the hub
	h.peers.bySID[peer.sid] = peer
	h.peers.byName[name] = peer
	atomic.StoreUint32(&peer.state, nmdcPeerJoining)
	h.globalChat.Join(peer)
	h.peers.Unlock()

	// notify other users about the new one
	h.broadcastUserJoin(peer, list)
	atomic.StoreUint32(&peer.state, nmdcPeerNormal)

	if h.conf.ChatLogJoin != 0 {
		h.globalChat.ReplayChat(peer, h.conf.ChatLogJoin)
	}

	if err := peer.conn.Flush(); err != nil {
		_ = peer.closeOn(list)
		return nil, err
	}

	return peer, nil
}

func (h *Hub) nmdcAccept(peer *nmdcPeer) error {
	deadline := time.Now().Add(time.Second * 5)

	c := peer.conn
	err := c.WriteMsg(&nmdc.Supports{
		Ext: peer.fea.List(),
	})
	if err != nil {
		return err
	}
	err = c.WriteMsg(&nmdc.HubName{
		Name: nmdc.Name(h.conf.Name),
	})
	if err != nil {
		return err
	}

	isRegistered, err := h.IsRegistered(peer.Name())
	if err != nil {
		return err
	}
	if isRegistered {
		// give the user a minute to enter a password
		deadline = time.Now().Add(time.Minute)
		err = c.WriteMsg(&nmdc.GetPass{})
		if err != nil {
			return err
		}
		err = c.Flush()
		if err != nil {
			return err
		}
		var pass nmdc.MyPass
		err = c.ReadMsgTo(deadline, &pass)
		if err != nil {
			return fmt.Errorf("expected password got: %v", err)
		}

		ok, err := h.nmdcCheckUserPass(peer.Name(), string(pass.String))
		if err != nil {
			return err
		} else if !ok {
			err = c.WriteMsg(&nmdc.BadPass{})
			if err != nil {
				return err
			}
			err = c.Flush()
			if err != nil {
				return err
			}
			return errors.New("wrong password")
		}
		deadline = time.Now().Add(time.Second * 5)
	}

	err = c.WriteMsg(&nmdc.Hello{
		Name: peer.user.Name,
	})
	if err != nil {
		return err
	}
	err = c.Flush()
	if err != nil {
		return err
	}

	var vers nmdc.Version
	err = c.ReadMsgTo(deadline, &vers)
	if err != nil {
		return fmt.Errorf("expected version: %v", err)
	} else if vers.Vers != "1,0091" && vers.Vers != "1.0091" && vers.Vers != "1,0098" {
		return fmt.Errorf("unexpected version: %q", vers)
	}
	var nicks nmdc.GetNickList
	err = c.ReadMsgTo(deadline, &nicks)
	if err != nil {
		return fmt.Errorf("expected version: %v", err)
	}
	curName := peer.user.Name
	err = c.ReadMyInfoTo(deadline, &peer.user)
	if err != nil {
		return err
	} else if curName != peer.user.Name {
		return errors.New("nick mismatch")
	}
	peer.setUser(&peer.user)

	err = c.WriteRaw(peer.userRaw)
	if err != nil {
		return err
	}
	err = c.WriteMsg(&nmdc.HubTopic{
		Text: h.conf.Desc,
	})
	if err != nil {
		return err
	}
	err = h.sendMOTD(peer)
	if err != nil {
		return err
	}

	if peer.fea.Has(nmdc.FeaUserCommand) {
		err = h.nmdcSendUserCommand(peer)
		if err != nil {
			return err
		}
	}

	// send user list (except his own info)
	err = peer.peersJoin(h.Peers(), true)
	if err != nil {
		return err
	}

	// write his info
	err = peer.peersJoin([]Peer{peer}, true)
	if err != nil {
		return err
	}
	// TODO: send the correct list once we supports ops
	err = c.WriteMsg(&nmdc.OpList{})
	if err != nil {
		return err
	}
	if peer.fea.Has(nmdc.FeaUserIP2) {
		err = c.WriteMsg(&nmdc.UserIP{
			Name: nmdc.Name(peer.Name()),
			IP:   peer.ip.String(),
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (h *Hub) nmdcCheckUserPass(name string, pass string) (bool, error) {
	if h.userDB == nil {
		return false, nil
	}
	exp, err := h.userDB.GetUserPassword(name)
	if err != nil {
		return false, err
	}
	return exp == pass, nil
}

func (h *Hub) nmdcServePeer(peer *nmdcPeer) error {
	peer.conn.KeepAlive(time.Minute / 2)
	verifyAddr := func(addr string) error {
		ip, port, err := net.SplitHostPort(addr)
		if err != nil {
			return fmt.Errorf("invalid address: %q", addr)
		}
		_, err = strconv.ParseUint(port, 10, 16)
		if err != nil {
			return fmt.Errorf("invalid port: %q", addr)
		}
		if ip != peer.ip.String() {
			return fmt.Errorf("invalid ip: %q vs %q", ip, peer.ip.String())
		}
		return nil
	}
	for {
		msg, err := peer.conn.ReadMsg(time.Time{})
		if err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}
		switch msg := msg.(type) {
		case *nmdc.ChatMessage:
			if string(msg.Name) != peer.Name() {
				return errors.New("invalid name in the chat message")
			}
			if strings.HasPrefix(msg.Text, "!") {
				sub := strings.SplitN(msg.Text, " ", 2)
				cmd := sub[0][1:]
				args := ""
				if len(sub) > 1 {
					args = sub[1]
				}
				h.command(peer, cmd, args)
				continue
			}
			h.globalChat.SendChat(peer, string(msg.Text))
		case *nmdc.GetNickList:
			list := h.Peers()
			peer.PeersJoin(list)
		case *nmdc.ConnectToMe:
			targ := h.byName(string(msg.Targ))
			if targ == nil {
				continue
			}
			if err := verifyAddr(msg.Address); err != nil {
				return fmt.Errorf("ctm: %v", err)
			}
			// TODO: token?
			h.connectReq(peer, targ, msg.Address, nmdcFakeToken, msg.Secure)
		case *nmdc.RevConnectToMe:
			if string(msg.From) != peer.Name() {
				return errors.New("invalid name in RevConnectToMe")
			}
			targ := h.byName(string(msg.To))
			if targ == nil {
				continue
			}
			h.revConnectReq(peer, targ, nmdcFakeToken, targ.User().TLS)
		case *nmdc.PrivateMessage:
			if name := peer.Name(); string(msg.From) != name || string(msg.Name) != name {
				return errors.New("invalid name in PrivateMessage")
			}
			to := string(msg.To)
			if strings.HasPrefix(to, "#") {
				// message in a chat room
				r := h.Room(to)
				if r == nil {
					continue
				}
				r.SendChat(peer, string(msg.Text))
			} else {
				// private message
				targ := h.byName(to)
				if targ == nil {
					continue
				}
				h.privateChat(peer, targ, Message{
					Name: string(msg.From),
					Text: string(msg.Text),
				})
			}
		case *nmdc.Search:
			if msg.Address != "" {
				if err := verifyAddr(msg.Address); err != nil {
					return fmt.Errorf("search: %v", err)
				}
			} else if msg.Nick != "" {
				if string(msg.Nick) != peer.Name() {
					return fmt.Errorf("search: invalid nick: %q", msg.Nick)
				}
			}
			h.nmdcHandleSearch(peer, msg)
		case *nmdc.SR:
			if string(msg.Source) != peer.Name() {
				return fmt.Errorf("search: invalid nick: %q", msg.Source)
			}
			to := h.byName(string(msg.Target))
			if to == nil {
				continue
			}
			h.nmdcHandleResult(peer, to, msg)
		default:
			// TODO
			data, _ := msg.MarshalNMDC(nil)
			log.Printf("%s: nmdc: $%s %v|", peer.RemoteAddr(), msg.Cmd(), string(data))
		}
	}
}

func (h *Hub) nmdcHandleSearch(peer *nmdcPeer, msg *nmdc.Search) {
	s := peer.newSearch(msg)
	if msg.DataType == nmdc.DataTypeTTH {
		// ignore other parameters
		h.SearchTTH(*msg.TTH, s)
		return
	}
	req := SearchReq{
		Pattern: msg.Pattern,
	}
	if msg.SizeRestricted {
		if msg.IsMaxSize {
			req.MaxSize = msg.Size
		} else {
			req.MinSize = msg.Size
		}
	}
	switch msg.DataType {
	case nmdc.DataTypeAudio:
		req.FileType = FileTypeAudio
	case nmdc.DataTypeCompressed:
		req.FileType = FileTypeCompressed
	case nmdc.DataTypeDocument:
		req.FileType = FileTypeDocuments
	case nmdc.DataTypeExecutable:
		req.FileType = FileTypeExecutable
	case nmdc.DataTypePicture:
		req.FileType = FileTypePicture
	case nmdc.DataTypeVideo:
		req.FileType = FileTypeVideo
	case nmdc.DataTypeFolders:
		req.FileType = FileTypeFolder
	}
	h.Search(req, s)
}

func (h *Hub) nmdcHandleResult(peer *nmdcPeer, to Peer, msg *nmdc.SR) {
	peer.search.RLock()
	cur := peer.search.peers[to]
	peer.search.RUnlock()

	if cur.out == nil {
		// not searching for anything
		return
	}
	var res SearchResult
	if msg.DirName != "" {
		res = Dir{Peer: peer, Path: msg.DirName}
	} else {
		res = File{Peer: peer, Path: msg.FileName, Size: msg.FileSize, TTH: msg.TTH}
	}
	if !cur.req.Validate(res) {
		return
	}
	if err := cur.out.SendResult(res); err != nil {
		_ = cur.out.Close()
		// TODO: remove from the map?
	}
}

func (h *Hub) nmdcSendUserCommand(peer *nmdcPeer) error {
	for _, c := range h.ListCommands() {
		path := make([]nmdc.String, 0, len(c.Path))
		for _, v := range c.Path {
			path = append(path, nmdc.String(v))
		}
		command := nmdc.String("<%[mynick]> !" + c.Name + "|")
		err := peer.conn.WriteMsg(&nmdc.UserCommand{
			Type:    nmdc.TypeRaw,
			Context: nmdc.ContextUser,
			Path:    path,
			Command: command,
		})
		if err != nil {
			return err
		}
	}
	return peer.conn.Flush()
}

var _ Peer = (*nmdcPeer)(nil)

const (
	nmdcPeerConnecting = iota
	nmdcPeerJoining
	nmdcPeerNormal
	nmdcPeerClosed
)

type nmdcPeer struct {
	BasePeer
	state uint32 // atomic

	conn *nmdc.Conn
	fea  nmdc.Features
	ip   net.IP

	mu      sync.RWMutex
	user    nmdc.MyInfo
	userRaw []byte
	closeMu sync.Mutex

	search struct {
		sync.RWMutex
		peers map[Peer]nmdcSearchRun
	}
}

type nmdcSearchRun struct {
	req *SearchReq // nil for TTH search
	out Search
}

func (p *nmdcPeer) getState() uint32 {
	return atomic.LoadUint32(&p.state)
}

func (p *nmdcPeer) setUser(u *nmdc.MyInfo) {
	if u != &p.user {
		p.user = *u
	}
	data, err := nmdc.Marshal(p.conn.TextEncoder(), u)
	if err != nil {
		panic(err)
	}
	p.userRaw = data
}

func (p *nmdcPeer) User() User {
	u := p.Info()
	return User{
		Name: string(u.Name),
		App: Software{
			Name: u.Client,
			Vers: u.Version,
		},
		HubsNormal:     u.HubsNormal,
		HubsRegistered: u.HubsRegistered,
		HubsOperator:   u.HubsOperator,
		Slots:          u.Slots,
		Email:          u.Email,
		Share:          u.ShareSize,
		IPv4:           u.Flag.IsSet(nmdc.FlagIPv4),
		IPv6:           u.Flag.IsSet(nmdc.FlagIPv6),
		TLS:            u.Flag.IsSet(nmdc.FlagTLS),
	}
}

func (p *nmdcPeer) Name() string {
	p.mu.RLock()
	name := p.user.Name
	p.mu.RUnlock()
	return string(name)
}

func (p *nmdcPeer) rawInfo() ([]byte, encoding.Encoding) {
	p.mu.RLock()
	data := p.userRaw
	p.mu.RUnlock()
	return data, p.conn.Encoding()
}

func (p *nmdcPeer) Info() nmdc.MyInfo {
	p.mu.RLock()
	u := p.user
	p.mu.RUnlock()
	return u
}

func (p *nmdcPeer) closeOn(list []Peer) error {
	switch p.getState() {
	case nmdcPeerClosed, nmdcPeerJoining:
		return nil
	}
	p.closeMu.Lock()
	defer p.closeMu.Unlock()
	p.mu.RLock()
	defer p.mu.RUnlock()
	switch p.getState() {
	case nmdcPeerClosed, nmdcPeerJoining:
		return nil
	}
	err := p.conn.Close()
	atomic.StoreUint32(&p.state, nmdcPeerClosed)

	name := string(p.user.Name)
	p.hub.leave(p, p.sid, name, list)
	return err
}

func (p *nmdcPeer) Close() error {
	return p.closeOn(nil)
}

func (p *nmdcPeer) writeOne(msg nmdc.Message) error {
	if p.getState() == nmdcPeerClosed {
		return errors.New("connection closed")
	}
	if err := p.conn.WriteOneMsg(msg); err != nil {
		_ = p.Close()
		return err
	}
	return nil
}

func (p *nmdcPeer) writeOneRaw(data []byte) error {
	if p.getState() == nmdcPeerClosed {
		return errors.New("connection closed")
	}
	if err := p.conn.WriteOneRaw(data); err != nil {
		_ = p.Close()
		return err
	}
	return nil
}

func (p *nmdcPeer) writeOneNow(msg nmdc.Message) error {
	if p.getState() == nmdcPeerClosed {
		return errors.New("connection closed")
	}
	// should only be used for closing the connection
	if err := p.conn.WriteMsg(msg); err != nil {
		return err
	}
	if err := p.conn.Flush(); err != nil {
		return err
	}
	return nil
}

func (p *nmdcPeer) failed(e error) error {
	return p.writeOneNow(&nmdc.Failed{Err: e})
}

func (p *nmdcPeer) error(e error) error {
	return p.writeOneNow(&nmdc.Error{Err: e})
}

func (p *nmdcPeer) BroadcastJoin(peers []Peer) {
	join, enc := p.rawInfo()
	for _, p2 := range peers {
		if p2, ok := p2.(*nmdcPeer); ok && enc == nil && p.conn.Encoding() == nil {
			_ = p2.writeOneRaw(join)
			continue
		}
		_ = p2.PeersJoin([]Peer{p})
	}
}

func (p *nmdcPeer) PeersJoin(peers []Peer) error {
	return p.peersJoin(peers, false)
}

func (u User) toNMDC() nmdc.MyInfo {
	flag := nmdc.FlagStatusNormal
	if u.IPv4 {
		flag |= nmdc.FlagIPv4
	}
	if u.IPv6 {
		flag |= nmdc.FlagIPv6
	}
	if u.TLS {
		flag |= nmdc.FlagTLS
	}
	return nmdc.MyInfo{
		Name:           nmdc.Name(u.Name),
		Client:         u.App.Name,
		Version:        u.App.Vers,
		HubsNormal:     u.HubsNormal,
		HubsRegistered: u.HubsRegistered,
		HubsOperator:   u.HubsOperator,
		Slots:          u.Slots,
		Email:          u.Email,
		ShareSize:      u.Share,
		Flag:           flag,

		// TODO
		Mode: nmdc.UserModeActive,
		Conn: "LAN(T3)",
	}
}

func (p *nmdcPeer) peersJoin(peers []Peer, initial bool) error {
	if p.getState() == nmdcPeerClosed {
		return errors.New("connection closed")
	}
	write := p.writeOne
	writeRaw := p.writeOneRaw
	if initial {
		write = p.conn.WriteMsg
		writeRaw = p.conn.WriteRaw
	}
	for _, peer := range peers {
		if p2, ok := peer.(*nmdcPeer); ok {
			data, enc := p2.rawInfo()
			if enc == nil && p.conn.Encoding() == nil {
				if err := writeRaw(data); err != nil {
					return err
				}
				continue
			}
		}
		info := peer.User().toNMDC()
		if err := write(&info); err != nil {
			return err
		}
	}
	return nil
}

func (p *nmdcPeer) BroadcastLeave(peers []Peer) {
	enc := p.conn.TextEncoder()
	quit, err := nmdc.Marshal(enc, &nmdc.Quit{
		Name: nmdc.Name(p.Name()),
	})
	if err != nil {
		panic(err)
	}
	for _, p2 := range peers {
		if p2, ok := p2.(*nmdcPeer); ok && enc == nil && p2.conn.TextEncoder() == nil {
			_ = p2.writeOneRaw(quit)
			continue
		}
		_ = p2.PeersLeave([]Peer{p})
	}
}

func (p *nmdcPeer) PeersLeave(peers []Peer) error {
	if p.getState() == nmdcPeerClosed {
		return errors.New("connection closed")
	}
	for _, peer := range peers {
		if err := p.writeOne(&nmdc.Quit{
			Name: nmdc.Name(peer.Name()),
		}); err != nil {
			return err
		}
	}
	return nil
}

func (p *nmdcPeer) JoinRoom(room *Room) error {
	rname := nmdc.Name(room.Name())
	if rname == "" {
		return nil
	}
	err := p.writeOne(&nmdc.MyInfo{
		Name:       rname,
		HubsNormal: room.Users(), // TODO: update
		Client:     p.hub.conf.Soft.Name,
		Version:    p.hub.conf.Soft.Vers,
		Mode:       nmdc.UserModeActive,
		Flag:       nmdc.FlagStatusServer,
		Slots:      1,
		Conn:       nmdc.ConnSpeedModem, // "modem" icon
	})
	if err != nil {
		return err
	}
	err = p.writeOne(&nmdc.OpList{
		Names: nmdc.Names{rname},
	})
	if err != nil {
		return err
	}
	return p.writeOne(&nmdc.PrivateMessage{
		From: rname, Name: rname,
		To:   nmdc.Name(p.Name()),
		Text: "joined the room",
	})
}

func (p *nmdcPeer) LeaveRoom(room *Room) error {
	rname := nmdc.Name(room.Name())
	if rname == "" {
		return nil
	}
	err := p.writeOne(&nmdc.PrivateMessage{
		From: rname, Name: rname,
		To:   nmdc.Name(p.Name()),
		Text: "left the room",
	})
	if err != nil {
		return err
	}
	return p.writeOne(&nmdc.Quit{
		Name: rname,
	})
}

func (p *nmdcPeer) ChatMsg(room *Room, from Peer, msg Message) error {
	rname := nmdc.Name(room.Name())
	if rname == "" {
		return p.writeOne(&nmdc.ChatMessage{
			Name: nmdc.Name(msg.Name),
			Text: msg.Text,
		})
	}
	if from == p {
		return nil // no echo
	}
	return p.writeOne(&nmdc.PrivateMessage{
		From: rname,
		To:   nmdc.Name(p.Name()),
		Name: nmdc.Name(msg.Name),
		Text: nmdc.String(msg.Text),
	})
}

func (p *nmdcPeer) PrivateMsg(from Peer, msg Message) error {
	fname := nmdc.Name(msg.Name)
	return p.writeOne(&nmdc.PrivateMessage{
		From: fname, Name: fname,
		To:   nmdc.Name(p.Name()),
		Text: nmdc.String(msg.Text),
	})
}

func (p *nmdcPeer) HubChatMsg(text string) error {
	return p.writeOne(&nmdc.ChatMessage{Text: text})
}

func (p *nmdcPeer) ConnectTo(peer Peer, addr string, token string, secure bool) error {
	// TODO: save token somewhere?
	return p.writeOne(&nmdc.ConnectToMe{
		Targ:    nmdc.Name(peer.Name()),
		Address: addr,
		Secure:  secure,
	})
}

func (p *nmdcPeer) RevConnectTo(peer Peer, token string, secure bool) error {
	// TODO: save token somewhere?
	return p.writeOne(&nmdc.RevConnectToMe{
		From: nmdc.Name(peer.Name()),
		To:   nmdc.Name(p.Name()),
	})
}

func (p *nmdcPeer) newSearch(req *nmdc.Search) Search {
	return &nmdcSearch{p: p, req: req}
}

type nmdcSearch struct {
	p   *nmdcPeer
	req *nmdc.Search
}

func (s *nmdcSearch) Peer() Peer {
	return s.p
}

func (s *nmdcSearch) SendResult(r SearchResult) error {
	h := s.p.hub
	// TODO: additional filtering?
	sr := &nmdc.SR{
		Source:    nmdc.Name(r.From().Name()),
		FreeSlots: 3, TotalSlots: 3, // TODO
		HubName:    nmdc.Name(h.Stats().Name),
		HubAddress: s.p.LocalAddr().String(),
	}
	switch r := r.(type) {
	case File:
		sr.FileName = r.Path
		sr.FileSize = r.Size
		if r.TTH != nil {
			sr.HubName = ""
			sr.TTH = r.TTH
		}
	case Dir:
		sr.DirName = r.Path
	default:
		return nil // ignore
	}
	return s.p.writeOne(sr)
}

func (s *nmdcSearch) Close() error {
	return nil // TODO: block new results
}

func (p *nmdcPeer) setActiveSearch(out Search, req *SearchReq) {
	p2 := out.Peer()
	p.search.Lock()
	defer p.search.Unlock()
	cur := p.search.peers[p2]
	if cur.out != nil {
		_ = cur.out.Close()
	}
	if p.search.peers == nil {
		p.search.peers = make(map[Peer]nmdcSearchRun)
	}
	p.search.peers[p2] = nmdcSearchRun{out: out, req: req}
}

func (p *nmdcPeer) Search(ctx context.Context, req SearchReq, out Search) error {
	p.setActiveSearch(out, &req)

	msg := &nmdc.Search{
		Nick:           nmdc.Name(out.Peer().Name()),
		Pattern:        req.Pattern,
		DataType:       nmdc.DataTypeAny,
		SizeRestricted: req.MinSize != 0 || req.MaxSize != 0,
	}
	if req.MaxSize != 0 {
		// prefer max size
		msg.IsMaxSize = true
		msg.Size = req.MaxSize
	} else if req.MinSize != 0 {
		msg.IsMaxSize = false
		msg.Size = req.MinSize
	}
	switch req.FileType {
	case FileTypeAudio:
		msg.DataType = nmdc.DataTypeAudio
	case FileTypeCompressed:
		msg.DataType = nmdc.DataTypeCompressed
	case FileTypeDocuments:
		msg.DataType = nmdc.DataTypeDocument
	case FileTypeExecutable:
		msg.DataType = nmdc.DataTypeExecutable
	case FileTypePicture:
		msg.DataType = nmdc.DataTypePicture
	case FileTypeVideo:
		msg.DataType = nmdc.DataTypeVideo
	case FileTypeFolder:
		msg.DataType = nmdc.DataTypeFolders
	}
	return p.writeOne(msg)
}

func (p *nmdcPeer) SearchTTH(ctx context.Context, tth TTH, out Search) error {
	p.setActiveSearch(out, nil) // TTH search

	return p.writeOne(&nmdc.Search{
		Nick:     nmdc.Name(out.Peer().Name()),
		DataType: nmdc.DataTypeTTH, TTH: &tth,
	})
}

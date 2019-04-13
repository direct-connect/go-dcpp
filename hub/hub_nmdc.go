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
	"time"

	nmdcp "github.com/direct-connect/go-dc/nmdc"
	"github.com/direct-connect/go-dcpp/nmdc"
)

var (
	errConnectionClosed = errors.New("connection closed")
)

const nmdcFakeToken = "nmdc"

func (h *Hub) serveNMDC(conn net.Conn) error {
	cntConnNMDC.Add(1)
	cntConnNMDCOpen.Add(1)
	defer cntConnNMDCOpen.Add(-1)
	log.Printf("%s: using NMDC", conn.RemoteAddr())
	return h.ServeNMDC(conn)
}

func (h *Hub) ServeNMDC(conn net.Conn) error {
	c, err := nmdc.NewConn(conn)
	if err != nil {
		return err
	}
	defer c.Close()
	c.SetWriteTimeout(writeTimeout)
	c.SetFallbackEncoding(h.fallback)
	c.OnLineR(func(line []byte) (bool, error) {
		sizeNMDCLinesR.Observe(float64(len(line)))
		return true, nil
	})
	c.OnLineW(func(line []byte) (bool, error) {
		sizeNMDCLinesW.Observe(float64(len(line)))
		return true, nil
	})
	c.OnUnmarshalError(func(line []byte, err error) (bool, error) {
		log.Printf("nmdc: failed to unmarshal:\n%q\n", string(line))
		return true, err
	})
	c.OnMessageR(func(m nmdcp.Message) (bool, error) {
		if _, ok := m.(*nmdcp.RawMessage); !ok {
			cntNMDCCommandsR.WithLabelValues(m.Type()).Add(1)
		} else {
			cntNMDCCommandsR.WithLabelValues("unknown").Add(1)
		}
		return true, nil
	})
	c.OnMessageW(func(m nmdcp.Message) (bool, error) {
		if _, ok := m.(*nmdcp.RawMessage); !ok {
			cntNMDCCommandsW.WithLabelValues(m.Type()).Add(1)
		} else {
			cntNMDCCommandsW.WithLabelValues("unknown").Add(1)
		}
		return true, nil
	})

	peer, err := h.nmdcHandshake(c)
	if err != nil {
		return err
	} else if peer == nil {
		return nil // pingers
	}
	defer peer.Close()
	return h.nmdcServePeer(peer)
}

func (h *Hub) nmdcLock(deadline time.Time, c *nmdc.Conn) (nmdcp.Extensions, string, error) {
	lock := &nmdcp.Lock{
		Lock: "_godcpp", // TODO: randomize
		PK:   h.conf.Soft.Name + " " + h.conf.Soft.Version,
	}
	err := c.WriteOneMsg(lock)
	if err != nil {
		return nil, "", err
	}

	var sup nmdcp.Supports
	err = c.ReadMsgTo(deadline, &sup)
	if err != nil {
		return nil, "", fmt.Errorf("expected supports: %v", err)
	}
	for _, ext := range sup.Ext {
		cntNMDCExtensions.WithLabelValues(ext).Add(1)
	}
	var key nmdcp.Key
	err = c.ReadMsgTo(deadline, &key)
	if err != nil {
		return nil, "", fmt.Errorf("expected key: %v", err)
	} else if key.Key != lock.Key().Key {
		return nil, "", errors.New("wrong key")
	}
	fea := make(nmdcp.Extensions, len(sup.Ext))
	for _, f := range sup.Ext {
		fea[f] = struct{}{}
	}
	if !fea.Has(nmdcp.ExtNoHello) {
		return nil, "", errors.New("NoHello is not supported")
	} else if !fea.Has(nmdcp.ExtNoGetINFO) {
		return nil, "", errors.New("NoGetINFO is not supported")
	}

	err = c.WriteOneMsg(&nmdcp.Supports{
		Ext: nmdcFeatures.List(),
	})
	if err != nil {
		return nil, "", err
	}

	nick, err := c.ReadValidateNick(deadline)
	if err != nil {
		return nil, "", err
	}
	return nmdcFeatures.Intersect(fea), string(nick.Name), nil
}

var nmdcFeatures = nmdcp.Extensions{
	nmdcp.ExtNoHello:     {},
	nmdcp.ExtNoGetINFO:   {},
	nmdcp.ExtBotINFO:     {},
	nmdcp.ExtTTHSearch:   {},
	nmdcp.ExtUserIP2:     {},
	nmdcp.ExtUserCommand: {},
	nmdcp.ExtTTHS:        {},
	nmdcp.ExtZPipe0:      {}, // see nmdc.Conn
}

func (h *Hub) nmdcHandshake(c *nmdc.Conn) (*nmdcPeer, error) {
	defer measure(durNMDCHandshake)()
	deadline := time.Now().Add(time.Second * 5)

	fea, nick, err := h.nmdcLock(deadline, c)
	if err != nil {
		_ = c.WriteOneMsg(&nmdcp.ChatMessage{Text: err.Error()})
		return nil, err
	}
	addr, ok := c.RemoteAddr().(*net.TCPAddr)
	if !ok {
		err = fmt.Errorf("not a tcp address: %T", c.RemoteAddr())
		_ = c.WriteOneMsg(&nmdcp.ChatMessage{Text: err.Error()})
		return nil, err
	}
	name := string(nick)
	err = h.validateUserName(name)
	if err != nil {
		_ = c.WriteOneMsg(&nmdcp.ChatMessage{Text: err.Error()})
		return nil, err
	}

	peer := newNMDC(h, c, fea, nick, addr.IP)

	if peer.fea.Has(nmdcp.ExtBotINFO) {
		cntPings.Add(1)
		cntPingsNMDC.Add(1)
		// it's a pinger - don't bother binding the nickname
		delete(peer.fea, nmdcp.ExtBotINFO)
		peer.fea.Set(nmdcp.ExtHubINFO)

		err = h.nmdcAccept(peer)
		if err != nil {
			return nil, err
		}
		var bot nmdcp.BotINFO
		if err := c.ReadMsgTo(deadline, &bot); err != nil {
			return nil, err
		}
		st := h.Stats()
		err = c.WriteMsg(&nmdcp.HubINFO{
			Name:     st.Name,
			Desc:     st.Desc,
			Host:     st.DefaultAddr(),
			Soft:     st.Soft,
			Encoding: "UTF8",
		})
		if err == nil {
			err = c.Flush()
		}
		return nil, err
	}

	// do not lock for writes first
	if !h.nameAvailable(name, nil) {
		_ = peer.c.WriteOneMsg(&nmdcp.ValidateDenide{nmdcp.Name(nick)})
		return nil, errNickTaken
	}

	// ok, now lock for writes and try to bind nick
	// still, no one will see the user yet
	unbind, ok := h.reserveName(name, nil, nil)
	if !ok {
		_ = peer.c.WriteOneMsg(&nmdcp.ValidateDenide{nmdcp.Name(nick)})
		return nil, errNickTaken
	}

	err = h.nmdcAccept(peer)
	if err != nil || !peer.Online() {
		unbind()

		str := "connection is closed"
		if err != nil {
			str = err.Error()
		}
		_ = peer.c.WriteOneMsg(&nmdcp.ChatMessage{Text: "handshake failed: " + str})
		return nil, err
	}

	var list []Peer
	// finally accept the user on the hub
	h.acceptPeer(peer, func() {
		// make a snapshot of peers to send info to
		list = h.listPeers()
	}, nil)

	// notify other users about the new one
	h.broadcastUserJoin(peer, list)

	if h.conf.ChatLogJoin != 0 {
		h.globalChat.ReplayChat(peer, h.conf.ChatLogJoin)
	}

	if err := peer.c.Flush(); err != nil {
		_ = peer.closeOn(list)
		return nil, err
	}

	return peer, nil
}

func (h *Hub) nmdcAccept(peer *nmdcPeer) error {
	deadline := time.Now().Add(time.Second * 5)

	c := peer.c
	err := c.WriteMsg(&nmdcp.HubName{
		String: nmdcp.String(h.conf.Name),
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
		err = c.WriteOneMsg(&nmdcp.GetPass{})
		if err != nil {
			return err
		}
		var pass nmdcp.MyPass
		err = c.ReadMsgTo(deadline, &pass)
		if err != nil {
			return fmt.Errorf("expected password got: %v", err)
		}

		ok, err := h.nmdcCheckUserPass(peer.Name(), string(pass.String))
		if err != nil {
			return err
		} else if !ok {
			err = c.WriteOneMsg(&nmdcp.BadPass{})
			if err != nil {
				return err
			}
			return errors.New("wrong password")
		}
		deadline = time.Now().Add(time.Second * 5)
	}

	err = c.WriteOneMsg(&nmdcp.Hello{
		Name: nmdcp.Name(peer.info.user.Name),
	})
	if err != nil {
		return err
	}

	var vers nmdcp.Version
	err = c.ReadMsgTo(deadline, &vers)
	if err != nil {
		return err
	} else if vers.Vers != "1,0091" && vers.Vers != "1.0091" && vers.Vers != "1,0098" {
		return fmt.Errorf("unexpected version: %q", vers)
	}
	curName := peer.info.user.Name

	// according to spec, we should only wait for GetNickList, but some clients
	// skip it and send MyINFO directly when reconnecting
	m, err := c.ReadMsgToAny(deadline, &nmdcp.GetNickList{}, &peer.info.user)
	if err != nil {
		return err
	}
	switch m.(type) {
	case *nmdcp.GetNickList:
		err = c.ReadMsgTo(deadline, &peer.info.user)
		if err != nil {
			return fmt.Errorf("expected user info: %v", err)
		}
	case *nmdcp.MyINFO:
		// already read to peer.user
	default:
		return fmt.Errorf("expected user info, got: %T", m)
	}
	cli := peer.info.user.Client
	cntClients.WithLabelValues(cli.Name, cli.Version).Add(1)
	if curName != peer.info.user.Name {
		return errors.New("nick mismatch")
	}

	peer.setUser(&peer.info.user)

	//err = c.WriteLine(peer.userRaw)
	//if err != nil {
	//	return err
	//}
	err = c.WriteMsg(&nmdcp.HubTopic{
		Text: h.conf.Desc,
	})
	if err != nil {
		return err
	}
	err = h.sendMOTD(peer)
	if err != nil {
		return err
	}

	if peer.fea.Has(nmdcp.ExtUserCommand) {
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
	err = c.WriteMsg(&nmdcp.OpList{})
	if err != nil {
		return err
	}
	if peer.fea.Has(nmdcp.ExtUserIP2) {
		err = c.WriteMsg(&nmdcp.UserIP{
			Name: peer.Name(),
			IP:   peer.ip.String(),
		})
		if err != nil {
			return err
		}
	}
	return c.Flush()
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
	if !h.callOnJoined(peer) {
		return nil // TODO: eny errors?
	}
	go peer.writer()
	for {
		msg, err := peer.c.ReadMsg(time.Time{})
		if err == io.EOF {
			return nil
		} else if err != nil {
			if !peer.Online() {
				return nil
			}
			return err
		}
		if err = h.nmdcHandle(peer, msg); err != nil {
			return err
		}
	}
}

func (h *Hub) nmdcHandle(peer *nmdcPeer, msg nmdcp.Message) error {
	defer measure(durNMDCHandle.WithLabelValues(msg.Type()))()

	switch msg := msg.(type) {
	case *nmdcp.ChatMessage:
		if string(msg.Name) != peer.Name() {
			return errors.New("invalid name in the chat message")
		}
		if h.isCommand(peer, msg.Text) {
			return nil
		}
		h.globalChat.SendChat(peer, string(msg.Text))
		return nil
	case *nmdcp.GetNickList:
		list := h.Peers()
		_ = peer.PeersJoin(list)
		return nil
	case *nmdcp.ConnectToMe:
		targ := h.PeerByName(string(msg.Targ))
		if targ == nil {
			return nil
		}
		if err := peer.verifyAddr(msg.Address); err != nil {
			return fmt.Errorf("ctm: %v", err)
		}
		if msg.Kind == nmdcp.CTMActive {
			// TODO: token?
			h.connectReq(peer, targ, msg.Address, nmdcFakeToken, msg.Secure)
			return nil
		}
		// NAT traversal
		if msg.Src != "" && msg.Src != peer.Name() {
			return errors.New("invalid name in the connect request")
		}
		p2, ok := targ.(*nmdcPeer)
		if !ok {
			return nil
		}
		return p2.SendNMDC(msg)
	case *nmdcp.RevConnectToMe:
		if string(msg.From) != peer.Name() {
			return errors.New("invalid name in RevConnectToMe")
		}
		targ := h.PeerByName(string(msg.To))
		if targ == nil {
			return nil
		}
		h.revConnectReq(peer, targ, nmdcFakeToken, targ.User().TLS)
		return nil
	case *nmdcp.PrivateMessage:
		if name := peer.Name(); string(msg.From) != name || string(msg.Name) != name {
			return errors.New("invalid name in PrivateMessage")
		}
		to := string(msg.To)
		if strings.HasPrefix(to, "#") {
			// message in a chat room
			r := h.Room(to)
			if r == nil {
				return nil
			}
			r.SendChat(peer, string(msg.Text))
		} else {
			// private message
			targ := h.PeerByName(to)
			if targ == nil {
				return nil
			}
			h.privateChat(peer, targ, Message{
				Name: string(msg.From),
				Text: string(msg.Text),
			})
		}
		return nil
	case *nmdcp.Search:
		if msg.Address != "" {
			if err := peer.verifyAddr(msg.Address); err != nil {
				return fmt.Errorf("search: %v", err)
			}
		} else if msg.User != "" {
			if string(msg.User) != peer.Name() {
				return fmt.Errorf("search: invalid nick: %q", msg.User)
			}
		}
		h.nmdcHandleSearch(peer, msg)
		return nil
	case *nmdcp.TTHSearchActive:
		if err := peer.verifyAddr(msg.Address); err != nil {
			return fmt.Errorf("search: %v", err)
		}
		h.nmdcHandleSearchTTH(peer, msg.TTH)
		return nil
	case *nmdcp.TTHSearchPassive:
		if string(msg.User) != peer.Name() {
			return fmt.Errorf("search: invalid nick: %q", msg.User)
		}
		h.nmdcHandleSearchTTH(peer, msg.TTH)
		return nil
	case *nmdcp.SR:
		if string(msg.From) != peer.Name() {
			return fmt.Errorf("search: invalid nick: %q", msg.From)
		}
		to := h.PeerByName(string(msg.To))
		if to == nil {
			return nil
		}
		h.nmdcHandleResult(peer, to, msg)
		return nil
	case *nmdcp.MyINFO:
		if string(msg.Name) != peer.Name() {
			return fmt.Errorf("myinfo: invalid nick: %q", msg.Name)
		}
		peer.SetInfo(msg)
		// TODO: notify about info update
		return nil
	default:
		// TODO
		data, _ := nmdcp.Marshal(nil, msg)
		log.Printf("%s: nmdc: %s", peer.RemoteAddr(), string(data))
		return nil
	}
}

func (h *Hub) nmdcHandleSearchTTH(peer *nmdcPeer, hash TTH) {
	s := peer.newSearch()
	h.Search(TTHSearch(hash), s, nil)
}

func (h *Hub) nmdcHandleSearch(peer *nmdcPeer, msg *nmdcp.Search) {
	// ignore some parameters - all searches will be delivered as passive
	s := peer.newSearch()
	if msg.DataType == nmdcp.DataTypeTTH {
		if peer.fea.Has(nmdcp.ExtTTHS) {
			// ignore duplicate Search requests from peers that support SP
			return
		}
		h.nmdcHandleSearchTTH(peer, *msg.TTH)
		return
	}
	var name NameSearch
	if p := strings.TrimSpace(msg.Pattern); p != "" {
		name.And = strings.Split(p, " ")
	}
	var req SearchRequest = name
	if msg.DataType == nmdcp.DataTypeFolders {
		req = DirSearch{name}
	} else if msg.DataType != nmdcp.DataTypeAny || msg.SizeRestricted {
		freq := FileSearch{NameSearch: name}
		if msg.SizeRestricted {
			if msg.IsMaxSize {
				freq.MaxSize = msg.Size
			} else {
				freq.MinSize = msg.Size
			}
		}
		switch msg.DataType {
		case nmdcp.DataTypeAudio:
			freq.FileType = FileTypeAudio
		case nmdcp.DataTypeCompressed:
			freq.FileType = FileTypeCompressed
		case nmdcp.DataTypeDocument:
			freq.FileType = FileTypeDocuments
		case nmdcp.DataTypeExecutable:
			freq.FileType = FileTypeExecutable
		case nmdcp.DataTypePicture:
			freq.FileType = FileTypePicture
		case nmdcp.DataTypeVideo:
			freq.FileType = FileTypeVideo
		}
		req = freq
	}
	h.Search(req, s, nil)
}

func (h *Hub) nmdcHandleResult(peer *nmdcPeer, to Peer, msg *nmdcp.SR) {
	peer.search.RLock()
	cur := peer.search.peers[to]
	peer.search.RUnlock()

	if cur.out == nil {
		// not searching for anything
		return
	}
	var res SearchResult
	path := strings.Join(msg.Path, "/")
	if msg.IsDir {
		res = Dir{Peer: peer, Path: path}
	} else {
		res = File{Peer: peer, Path: path, Size: msg.Size, TTH: msg.TTH}
	}
	if !cur.req.Match(res) {
		return
	}
	if err := cur.out.SendResult(res); err != nil {
		_ = cur.out.Close()
		// TODO: remove from the map?
	}
}

func (h *Hub) nmdcSendUserCommand(peer *nmdcPeer) error {
	for _, c := range h.ListCommands() {
		err := peer.c.WriteMsg(&nmdcp.UserCommand{
			Typ:     nmdcp.TypeRaw,
			Context: nmdcp.ContextUser,
			Path:    c.Menu,
			Command: "<%[mynick]> !" + c.Name + "|",
		})
		if err != nil {
			return err
		}
	}
	return nil
}

var _ Peer = (*nmdcPeer)(nil)

const (
	nmdcPeerConnecting = iota
	nmdcPeerJoining
	nmdcPeerNormal
	nmdcPeerClosed
)

func newNMDC(h *Hub, c *nmdc.Conn, fea nmdcp.Extensions, nick string, ip net.IP) *nmdcPeer {
	peer := &nmdcPeer{
		BasePeer: BasePeer{
			hub:      h,
			hubAddr:  c.LocalAddr(),
			peerAddr: c.RemoteAddr(),
			sid:      h.nextSID(),
		},
		c: c, ip: ip,
		fea: fea,
	}
	peer.close.done = make(chan struct{})
	peer.write.wake = make(chan struct{}, 1)
	peer.info.user.Name = nick
	return peer
}

type nmdcPeer struct {
	BasePeer

	c   *nmdc.Conn
	fea nmdcp.Extensions
	ip  net.IP

	close struct {
		sync.Mutex
		done chan struct{}
	}
	write struct {
		wake chan struct{}
		sync.Mutex
		buf []nmdcp.Message
	}
	info struct {
		sync.RWMutex
		user nmdcp.MyINFO
	}

	search struct {
		sync.RWMutex
		peers map[Peer]nmdcSearchRun
	}
}

type nmdcSearchRun struct {
	req SearchRequest
	out Search
}

func (p *nmdcPeer) SetInfo(u *nmdcp.MyINFO) {
	p.info.Lock()
	defer p.info.Unlock()
	p.setUser(u)
}

func (p *nmdcPeer) setUser(u *nmdcp.MyINFO) {
	if u != &p.info.user {
		p.info.user = *u
	}
	//data, err := nmdcp.Marshal(p.conn.TextEncoder(), u)
	//if err != nil {
	//	panic(err)
	//}
	//p.userRaw = data
}

func (p *nmdcPeer) User() User {
	u := p.Info()
	return User{
		Name:           string(u.Name),
		App:            u.Client,
		HubsNormal:     u.HubsNormal,
		HubsRegistered: u.HubsRegistered,
		HubsOperator:   u.HubsOperator,
		Slots:          u.Slots,
		Email:          u.Email,
		Share:          u.ShareSize,
		IPv4:           u.Flag.IsSet(nmdcp.FlagIPv4),
		IPv6:           u.Flag.IsSet(nmdcp.FlagIPv6),
		TLS:            u.Flag.IsSet(nmdcp.FlagTLS),
	}
}

func (p *nmdcPeer) Name() string {
	p.info.RLock()
	name := p.info.user.Name
	p.info.RUnlock()
	return string(name)
}

//func (p *nmdcPeer) rawInfo() ([]byte, encoding.Encoding) {
//	p.mu.RLock()
//	data := p.userRaw
//	p.mu.RUnlock()
//	return data, p.conn.Encoding()
//}

func (p *nmdcPeer) Info() nmdcp.MyINFO {
	p.info.RLock()
	u := p.info.user
	p.info.RUnlock()
	return u
}

func (p *nmdcPeer) closeOn(list []Peer) error {
	if !p.Online() {
		return nil
	}
	p.close.Lock()
	defer p.close.Unlock()
	if !p.Online() {
		return nil
	}
	close(p.close.done)
	p.setOffline()
	err := p.c.Close()

	p.hub.leave(p, p.sid, list)
	return err
}

func (p *nmdcPeer) Close() error {
	return p.closeOn(nil)
}

func (p *nmdcPeer) writer() {
	defer p.Close()
	ticker := time.NewTicker(time.Minute / 2)
	defer ticker.Stop()

	var buf2 []nmdcp.Message
	for {
		var err error
		select {
		case <-p.close.done:
			return
		case <-ticker.C:
			// keep alive
			err = p.c.WriteLine([]byte("|"))
		case <-p.write.wake:
			p.write.Lock()
			buf := p.write.buf
			p.write.buf = buf2
			p.write.Unlock()
			if len(buf) == 0 {
				buf2 = buf[:0]
				continue
			}
			for _, m := range buf {
				err = p.c.WriteMsg(m)
				if err != nil {
					break
				}
			}
			buf2 = buf[:0]
		}
		if err == nil {
			err = p.c.Flush()
		}
		if err != nil {
			if p.Online() {
				log.Printf("%s: write: %v", p.c.RemoteAddr(), err)
			}
			return
		}
	}
}

func (p *nmdcPeer) SendNMDC(m ...nmdcp.Message) error {
	if !p.Online() {
		return errConnectionClosed
	}
	p.write.Lock()
	if !p.Online() {
		p.write.Unlock()
		return errConnectionClosed
	}
	p.write.buf = append(p.write.buf, m...)
	p.write.Unlock()
	select {
	case p.write.wake <- struct{}{}:
	default:
	}
	return nil
}

func (p *nmdcPeer) verifyAddr(addr string) error {
	ip, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid address: %q", addr)
	}
	_, err = strconv.ParseUint(port, 10, 16)
	if err != nil {
		return fmt.Errorf("invalid port: %q", addr)
	}
	if ip != p.ip.String() {
		return fmt.Errorf("invalid ip: %q vs %q", ip, p.ip.String())
	}
	return nil
}

func (p *nmdcPeer) BroadcastJoin(peers []Peer) {
	//join, enc := p.rawInfo()
	for _, p2 := range peers {
		//if p2, ok := p2.(*nmdcPeer); ok && enc == nil && p.conn.Encoding() == nil {
		//	_ = p2.writeLineAsync(join)
		//	continue
		//}
		_ = p2.PeersJoin([]Peer{p})
	}
}

func (p *nmdcPeer) PeersJoin(peers []Peer) error {
	return p.peersJoin(peers, false)
}

func (u User) toNMDC() nmdcp.MyINFO {
	flag := nmdcp.FlagStatusNormal
	if u.IPv4 {
		flag |= nmdcp.FlagIPv4
	}
	if u.IPv6 {
		flag |= nmdcp.FlagIPv6
	}
	if u.TLS {
		flag |= nmdcp.FlagTLS
	}
	return nmdcp.MyINFO{
		Name:           u.Name,
		Client:         u.App,
		HubsNormal:     u.HubsNormal,
		HubsRegistered: u.HubsRegistered,
		HubsOperator:   u.HubsOperator,
		Slots:          u.Slots,
		Email:          u.Email,
		ShareSize:      u.Share,
		Flag:           flag,

		// TODO
		Mode: nmdcp.UserModeActive,
		Conn: "LAN(T3)",
	}
}

func (p *nmdcPeer) peersJoin(peers []Peer, initial bool) error {
	for _, peer := range peers {
		if !p.Online() {
			return errConnectionClosed
		}
		//if p2, ok := peer.(*nmdcPeer); ok {
		//	data, enc := p2.rawInfo()
		//	if enc == nil && p.conn.Encoding() == nil {
		//		if err := p.conn.WriteLine(data); err != nil {
		//			return err
		//		}
		//		continue
		//	}
		//}
		info := peer.User().toNMDC()
		var err error
		if initial {
			err = p.c.WriteMsg(&info)
		} else {
			err = p.SendNMDC(&info)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (p *nmdcPeer) BroadcastLeave(peers []Peer) {
	//enc := p.conn.TextEncoder()
	//quit, err := nmdcp.Marshal(enc, &nmdcp.Quit{
	//	Name: nmdcp.Name(p.Name()),
	//})
	//if err != nil {
	//	panic(err)
	//}
	for _, p2 := range peers {
		//if p2, ok := p2.(*nmdcPeer); ok && enc == nil && p2.conn.TextEncoder() == nil {
		//	_ = p2.writeLineAsync(quit)
		//	continue
		//}
		_ = p2.PeersLeave([]Peer{p})
	}
}

func (p *nmdcPeer) PeersLeave(peers []Peer) error {
	if len(peers) == 0 {
		return nil
	} else if !p.Online() {
		return errConnectionClosed
	}
	cmds := make([]nmdcp.Message, 0, len(peers))
	for _, peer := range peers {
		if !p.Online() {
			return errConnectionClosed
		}
		cmds = append(cmds, &nmdcp.Quit{
			Name: nmdcp.Name(peer.Name()),
		})
	}
	return p.SendNMDC(cmds...)
}

func (p *nmdcPeer) JoinRoom(room *Room) error {
	if !p.Online() {
		return errConnectionClosed
	}
	rname := room.Name()
	if rname == "" {
		return nil
	}
	return p.SendNMDC(
		&nmdcp.MyINFO{
			Name:       rname,
			HubsNormal: room.Users(), // TODO: update
			Client:     p.hub.conf.Soft,
			Mode:       nmdcp.UserModeActive,
			Flag:       nmdcp.FlagStatusServer,
			Slots:      1,
			Conn:       nmdcp.ConnSpeedModem, // "modem" icon
		},
		&nmdcp.OpList{
			Names: nmdcp.Names{rname},
		},
		&nmdcp.PrivateMessage{
			From: rname, Name: rname,
			To:   p.Name(),
			Text: "joined the room",
		},
	)
}

func (p *nmdcPeer) LeaveRoom(room *Room) error {
	if !p.Online() {
		return errConnectionClosed
	}
	rname := room.Name()
	if rname == "" {
		return nil
	}
	return p.SendNMDC(
		&nmdcp.PrivateMessage{
			From: rname, Name: rname,
			To:   p.Name(),
			Text: "left the room",
		},
		&nmdcp.Quit{
			Name: nmdcp.Name(rname),
		},
	)
}

func (p *nmdcPeer) ChatMsg(room *Room, from Peer, msg Message) error {
	if !p.Online() {
		return errConnectionClosed
	}
	rname := room.Name()
	if rname == "" {
		return p.SendNMDC(&nmdcp.ChatMessage{
			Name: msg.Name,
			Text: msg.Text,
		})
	}
	if from == p {
		return nil // no echo
	}
	return p.SendNMDC(&nmdcp.PrivateMessage{
		From: rname,
		To:   p.Name(),
		Name: msg.Name,
		Text: msg.Text,
	})
}

func (p *nmdcPeer) PrivateMsg(from Peer, msg Message) error {
	if !p.Online() {
		return errConnectionClosed
	}
	fname := msg.Name
	return p.SendNMDC(&nmdcp.PrivateMessage{
		From: fname, Name: fname,
		To:   p.Name(),
		Text: msg.Text,
	})
}

func (p *nmdcPeer) HubChatMsg(text string) error {
	if !p.Online() {
		return errConnectionClosed
	}
	return p.SendNMDC(&nmdcp.ChatMessage{Name: p.hub.conf.Name, Text: text})
}

func (p *nmdcPeer) ConnectTo(peer Peer, addr string, token string, secure bool) error {
	if !p.Online() {
		return errConnectionClosed
	}
	// TODO: save token somewhere?
	return p.SendNMDC(&nmdcp.ConnectToMe{
		Targ:    peer.Name(),
		Address: addr,
		Secure:  secure,
	})
}

func (p *nmdcPeer) RevConnectTo(peer Peer, token string, secure bool) error {
	if !p.Online() {
		return errConnectionClosed
	}
	// TODO: save token somewhere?
	return p.SendNMDC(&nmdcp.RevConnectToMe{
		From: peer.Name(),
		To:   p.Name(),
	})
}

func (p *nmdcPeer) newSearch() Search {
	return &nmdcSearch{p: p}
}

type nmdcSearch struct {
	p *nmdcPeer
}

func (s *nmdcSearch) Peer() Peer {
	return s.p
}

func (s *nmdcSearch) SendResult(r SearchResult) error {
	if !s.p.Online() {
		return errConnectionClosed
	}
	h := s.p.hub
	// TODO: additional filtering?
	sr := &nmdcp.SR{
		From:      r.From().Name(),
		FreeSlots: 3, TotalSlots: 3, // TODO
		HubName:    h.Stats().Name,
		HubAddress: s.p.LocalAddr().String(),
	}
	switch r := r.(type) {
	case File:
		sr.Path = strings.Split(r.Path, "/")
		sr.Size = r.Size
		if r.TTH != nil {
			sr.HubName = ""
			sr.TTH = r.TTH
		}
	case Dir:
		sr.Path = strings.Split(r.Path, "/")
		sr.IsDir = true
	default:
		return nil // ignore
	}
	return s.p.SendNMDC(sr)
}

func (s *nmdcSearch) Close() error {
	return nil // TODO: block new results
}

func (p *nmdcPeer) setActiveSearch(out Search, req SearchRequest) {
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

func (p *nmdcPeer) Search(ctx context.Context, req SearchRequest, out Search) error {
	if !p.Online() {
		return errConnectionClosed
	}
	p.setActiveSearch(out, req)
	if req, ok := req.(TTHSearch); ok {
		if p.fea.Has(nmdcp.ExtTTHS) {
			return p.SendNMDC(&nmdcp.TTHSearchPassive{
				User: out.Peer().Name(),
				TTH:  TTH(req),
			})
		}
		return p.SendNMDC(&nmdcp.Search{
			User:     out.Peer().Name(),
			DataType: nmdcp.DataTypeTTH, TTH: (*TTH)(&req),
		})
	}

	msg := &nmdcp.Search{
		User:     out.Peer().Name(),
		DataType: nmdcp.DataTypeAny,
	}
	var name NameSearch
	switch req := req.(type) {
	case NameSearch:
		name = req
	case DirSearch:
		name = req.NameSearch
		msg.DataType = nmdcp.DataTypeFolders
	case FileSearch:
		name = req.NameSearch
		if req.MaxSize != 0 {
			// prefer max size
			msg.SizeRestricted = true
			msg.IsMaxSize = true
			msg.Size = req.MaxSize
		} else if req.MinSize != 0 {
			msg.SizeRestricted = true
			msg.IsMaxSize = false
			msg.Size = req.MinSize
		}
		switch req.FileType {
		case FileTypeAudio:
			msg.DataType = nmdcp.DataTypeAudio
		case FileTypeCompressed:
			msg.DataType = nmdcp.DataTypeCompressed
		case FileTypeDocuments:
			msg.DataType = nmdcp.DataTypeDocument
		case FileTypeExecutable:
			msg.DataType = nmdcp.DataTypeExecutable
		case FileTypePicture:
			msg.DataType = nmdcp.DataTypePicture
		case FileTypeVideo:
			msg.DataType = nmdcp.DataTypeVideo
		}
	default:
		return nil // ignore
	}
	msg.Pattern += strings.Join(name.And, " ")
	return p.SendNMDC(msg)
}

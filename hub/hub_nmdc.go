package hub

import (
	"bytes"
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

	nmdcp "github.com/direct-connect/go-dc/nmdc"
	"github.com/direct-connect/go-dcpp/nmdc"
)

var (
	errConnectionClosed = errors.New("connection closed")
)

const (
	nmdcFakeToken = "nmdc"
	nmdcMaxPerMin = 30
)

var nmdcMaxPerMinCmd = map[string]uint{
	(&nmdcp.MyINFO{}).Type():           30,
	(&nmdcp.TTHSearchPassive{}).Type(): 15,
	(&nmdcp.TTHSearchActive{}).Type():  15,
	(&nmdcp.Search{}).Type():           15,
	(&nmdcp.SR{}).Type():               150,
	(&nmdcp.ConnectToMe{}).Type():      20,
	(&nmdcp.RevConnectToMe{}).Type():   20,
}

func (h *Hub) ServeNMDC(conn net.Conn, cinfo *ConnInfo) error {
	cntConnNMDC.Add(1)
	cntConnNMDCOpen.Add(1)
	defer cntConnNMDCOpen.Add(-1)

	if cinfo.TLSVers != 0 {
		cntConnNMDCS.Add(1)
	}
	if cinfo.ALPN != "" {
		cntConnAlpnNMDC.Add(1)
	}
	if cinfo == nil {
		cinfo = &ConnInfo{Local: conn.LocalAddr(), Remote: conn.RemoteAddr()}
	}

	log.Printf("%s: using NMDC", conn.RemoteAddr())

	c, err := nmdc.NewConn(conn)
	if err != nil {
		return err
	}
	defer c.Close()
	_ = c.SetWriteDeadline(time.Now().Add(writeTimeout))
	c.SetFallbackEncoding(h.fallback)
	c.OnLineR(func(line []byte) (bool, error) {
		sizeNMDCLinesR.Observe(float64(len(line)))
		if h.sampler.enabled() {
			h.sampler.sample(line)
		}
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
	c.OnRawMessageR(func(cmd, data []byte) (bool, error) {
		cnt, ok := sizeNMDCCommandR[string(cmd)]
		if !ok {
			cnt = sizeNMDCCommandR[cmdUnknown]
		}
		if cnt != nil {
			n := 1
			if len(cmd) != 0 {
				n += 1 + len(cmd)
			}
			if len(data) != 0 {
				n += 1 + len(data)
			}
			cnt.Observe(float64(n))
		}
		return true, nil
	})
	c.OnMessageR(func(m nmdcp.Message) (bool, error) {
		countM(cntNMDCCommandsR, m.Type(), 1)
		return true, nil
	})
	c.OnMessageW(func(m nmdcp.Message) (bool, error) {
		countM(cntNMDCCommandsW, m.Type(), 1)
		return true, nil
	})

	peer, err := h.nmdcHandshake(c, cinfo)
	if err != nil {
		return err
	} else if peer == nil {
		return nil // pingers
	}
	defer peer.Close()
	return h.nmdcServePeer(peer)
}

func (h *Hub) nmdcLock(deadline time.Time, c *nmdc.Conn) (nmdcp.Extensions, string, error) {
	soft := h.getSoft()
	lock := &nmdcp.Lock{
		Lock: "_godcpp", // TODO: randomize
		PK:   soft.Name + " " + soft.Version,
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
	nmdcp.ExtBotList:     {},
	nmdcp.ExtZPipe0:      {}, // see nmdc.Conn
}

func (h *Hub) nmdcHandshake(c *nmdc.Conn, cinfo *ConnInfo) (*nmdcPeer, error) {
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

	peer := newNMDC(h, cinfo, c, fea, nick, addr.IP)

	if peer.fea.Has(nmdcp.ExtBotINFO) {
		cntPings.Add(1)
		cntPingsNMDC.Add(1)
		// it's a pinger - don't bother binding the nickname
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
			Encoding: "UTF-8",
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
		String: nmdcp.String(h.getName()),
	})
	if err != nil {
		return err
	}

	user, rec, err := h.getUser(peer.Name())
	if err != nil {
		return err
	}
	if user != nil && rec != nil {
		if c := peer.ConnInfo(); c != nil && !c.Secure {
			return errConnInsecure
		}
		// give the user a minute to enter a password
		deadline = time.Now().Add(time.Minute)
		_ = c.SetWriteDeadline(deadline)
		err = c.WriteOneMsg(&nmdcp.GetPass{})
		if err != nil {
			return err
		}
		var pass nmdcp.MyPass
		err = c.ReadMsgTo(deadline, &pass)
		if err != nil {
			return fmt.Errorf("expected password got: %v", err)
		}

		ok, err := h.nmdcCheckUserPass(rec, string(pass.String))
		if err != nil {
			return err
		} else if !ok {
			err = c.WriteOneMsg(&nmdcp.BadPass{})
			if err != nil {
				return err
			}
			return errors.New("wrong password")
		}
		peer.setUser(user)
		deadline = time.Now().Add(time.Second * 5)
	}

	_ = c.SetWriteDeadline(deadline)
	err = c.WriteMsg(&nmdcp.Hello{
		Name: nmdcp.Name(peer.info.user.Name),
	})
	if err != nil {
		return err
	}
	if lvl := h.zlibLevel(); lvl != 0 && peer.fea.Has(nmdcp.ExtZPipe0) {
		err = c.ZOn(lvl) // flushes
	} else {
		err = c.Flush()
	}
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

	peer.setUserInfo(&peer.info.user)

	err = c.WriteMsg(&nmdcp.HubTopic{
		Text: h.getTopic(),
	})
	if err != nil {
		return err
	}
	err = peer.HubChatMsg(Message{Text: h.poweredBy()})
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

	_ = c.SetWriteDeadline(time.Now().Add(writeTimeout))
	// send user list (except his own info)
	peers := h.Peers()
	err = peer.peersJoin(&PeersJoinEvent{Peers: peers}, true)
	if err != nil {
		return err
	}

	// write his info
	err = peer.peersJoin(&PeersJoinEvent{Peers: []Peer{peer}}, true)
	if err != nil {
		return err
	}
	var ops, bots nmdcp.Names
	if u := peer.User(); u != nil && u.Has(FlagOpIcon) {
		ops = append(ops, peer.Name())
	}
	for _, p := range peers {
		if u := p.User(); u != nil && u.Has(FlagOpIcon) {
			ops = append(ops, p.Name())
		}
		if peer.ext.botlist {
			if info := p.UserInfo(); info.Kind == UserBot || info.Kind == UserHub {
				bots = append(bots, p.Name())
			}
		}
	}
	err = c.WriteMsg(&nmdcp.OpList{Names: ops})
	if err != nil {
		return err
	}
	if peer.ext.botlist && len(bots) != 0 {
		err = c.WriteMsg(&nmdcp.BotList{Names: bots})
		if err != nil {
			return err
		}
	}
	if peer.ext.userip2 {
		err = c.WriteMsg(&nmdcp.UserIP{
			List: []nmdcp.UserAddress{{
				Name: peer.Name(),
				IP:   peer.ip.String(),
			}},
		})
		if err != nil {
			return err
		}
		if peer.User().HasPerm(PermIP) {
			var ips []nmdcp.UserAddress
			for _, p := range peers {
				addr, ok := p.RemoteAddr().(*net.TCPAddr)
				if !ok {
					continue
				}
				ips = append(ips, nmdcp.UserAddress{
					Name: p.Name(),
					IP:   addr.IP.String(),
				})
			}
			if len(ips) != 0 {
				err = c.WriteMsg(&nmdcp.UserIP{List: ips})
				if err != nil {
					return err
				}
			}
		}
	}
	_ = c.SetWriteDeadline(time.Now().Add(writeTimeout))
	return c.Flush()
}

func (h *Hub) nmdcCheckUserPass(rec *UserRecord, pass string) (bool, error) {
	if h.db == nil {
		return false, nil
	}
	return rec.Pass == pass, nil
}

func (h *Hub) nmdcServePeer(peer *nmdcPeer) error {
	if !h.callOnJoined(peer) {
		return nil // TODO: eny errors?
	}
	// looks like we are disabling the timeout, but we are not
	// the timeout will be set manually by the writer goroutine
	peer.c.SetWriteTimeout(-1)
	go peer.writer(writeTimeout)

	cnt := make(map[string]uint)
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
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
		typ := msg.Type()
		if !nmdcp.IsRegistered(typ) {
			countM(cntNMDCCommandsDrop, typ, 1)
			continue
		}
		select {
		case <-ticker.C:
			for k := range cnt {
				cnt[k] = 0
			}
		default:
		}
		n := cnt[typ]
		n++
		cnt[typ] = n

		max := uint(nmdcMaxPerMin)
		if v, ok := nmdcMaxPerMinCmd[typ]; ok {
			max = v
		}
		if n >= max {
			countM(cntNMDCCommandsDrop, typ, 1)
			if n == max {
				log.Println("flood:", peer.Name(), typ, msg)
			}
			// TODO: temp ban?
			continue
		}
		if err = h.nmdcHandle(peer, msg); err != nil {
			countM(cntNMDCCommandsDrop, typ, 1)
			return err
		}
	}
}

func (h *Hub) nmdcHandle(peer *nmdcPeer, msg nmdcp.Message) error {
	typ := msg.Type()
	defer measureM(durNMDCHandle, typ)()

	switch msg := msg.(type) {
	case *nmdcp.ChatMessage:
		if string(msg.Name) != peer.Name() {
			return errors.New("invalid name in the chat message")
		}
		if h.isCommand(peer, msg.Text) {
			return nil
		}
		m := Message{
			Name: string(msg.Name),
			Text: string(msg.Text),
		}
		if m.Text == "/me" {
			m.Me = true
			m.Text = ""
		} else if strings.HasPrefix(m.Text, "/me ") {
			m.Me = true
			m.Text = m.Text[4:]
		}
		h.globalChat.SendChat(peer, m)
		return nil
	case *nmdcp.GetNickList:
		list := h.Peers()
		_ = peer.PeersJoin(&PeersJoinEvent{Peers: list})
		return nil
	case *nmdcp.ConnectToMe:
		targ := h.PeerByName(string(msg.Targ))
		if targ == nil || targ == peer {
			countM(cntNMDCCommandsDrop, typ, 1)
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
			countM(cntNMDCCommandsDrop, typ, 1)
			return nil
		}
		return p2.SendNMDC(msg)
	case *nmdcp.RevConnectToMe:
		if string(msg.From) != peer.Name() {
			return errors.New("invalid name in RevConnectToMe")
		}
		targ := h.PeerByName(string(msg.To))
		if targ == nil || targ == peer {
			countM(cntNMDCCommandsDrop, typ, 1)
			return nil
		}
		h.revConnectReq(peer, targ, nmdcFakeToken, targ.UserInfo().TLS)
		return nil
	case *nmdcp.PrivateMessage:
		if name := peer.Name(); string(msg.From) != name || string(msg.Name) != name {
			return errors.New("invalid name in PrivateMessage")
		}
		to := string(msg.To)
		m := Message{
			Name: string(msg.From),
			Text: string(msg.Text),
		}
		if m.Text == "/me" {
			m.Me = true
			m.Text = ""
		} else if strings.HasPrefix(m.Text, "/me ") {
			m.Me = true
			m.Text = m.Text[4:]
		}
		if strings.HasPrefix(to, "#") {
			// message in a chat room
			r := h.Room(to)
			if r == nil {
				countM(cntNMDCCommandsDrop, typ, 1)
				return nil
			}
			r.SendChat(peer, m)
		} else {
			// private message
			targ := h.PeerByName(to)
			if targ == nil {
				countM(cntNMDCCommandsDrop, typ, 1)
				return nil
			}
			h.privateChat(peer, targ, m)
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
			countM(cntNMDCCommandsDrop, typ, 1)
			return nil
		}
		h.nmdcHandleResult(peer, to, msg)
		return nil
	case *nmdcp.MyINFO:
		if string(msg.Name) != peer.Name() {
			return fmt.Errorf("myinfo: invalid nick: %q", msg.Name)
		} else if u := peer.Info(); u.Client != msg.Client {
			return errors.New("client masquerade is not allowed")
		}
		peer.SetInfo(msg)
		h.broadcastUserUpdate(peer, nil)
		return nil
	default:
		countM(cntNMDCCommandsDrop, typ, 1)
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
	if msg.DataType == nmdcp.DataTypeTTH {
		if peer.fea.Has(nmdcp.ExtTTHS) {
			// ignore duplicate Search requests from peers that supports SP
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
	s := peer.newSearch()
	h.Search(req, s, nil)
}

func (h *Hub) nmdcHandleResult(peer *nmdcPeer, to Peer, msg *nmdcp.SR) {
	peer.search.RLock()
	cur := peer.search.peers[to]
	peer.search.RUnlock()

	if cur == nil || cur.out == nil {
		// not searching for anything
		return
	}
	atomic.StoreInt64(&cur.last, time.Now().Unix())
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
	for _, c := range h.ListCommands(peer.User()) {
		cat := nmdcp.ContextHub
		cmd := "<%[mynick]> !" + c.Name
		if c.opt.OnUser {
			cmd += " %[nick]"
			cat = nmdcp.ContextUser
		}
		cmd += "|"
		err := peer.c.WriteMsg(&nmdcp.UserCommand{
			Typ:     nmdcp.TypeRaw,
			Context: cat,
			Path:    c.Menu,
			Command: cmd,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (h *Hub) sendNMDCTo(p Peer, m nmdcp.Message) error {
	switch m := m.(type) {
	case *nmdcp.ChatMessage:
		from := h.PeerByName(m.Name)
		if from == nil {
			return fmt.Errorf("cannot find the message author: '%s'", m.Name)
		}
		if np, ok := p.(*nmdcPeer); ok {
			return np.SendNMDC(m)
		}
		msg := Message{Time: time.Now(), Name: m.Name, Text: m.Text}
		_ = p.ChatMsg(nil, from, msg)
		return nil
	default:
		if np, ok := p.(*nmdcPeer); ok {
			return np.SendNMDC(m)
		}
		log.Printf("TODO: Hub.SendNMDC(%T, %T)", p, m)
		return nil
	}
}

func (h *Hub) SendNMDCTo(p Peer, msgs ...nmdcp.Message) error {
	for _, m := range msgs {
		if err := h.sendNMDCTo(p, m); err != nil && err != errConnectionClosed {
			return err
		}
	}
	return nil
}

var (
	_ Peer      = (*nmdcPeer)(nil)
	_ PeerTopic = (*nmdcPeer)(nil)
)

func newNMDC(h *Hub, cinfo *ConnInfo, c *nmdc.Conn, fea nmdcp.Extensions, nick string, ip net.IP) *nmdcPeer {
	if cinfo == nil {
		cinfo = &ConnInfo{Local: c.LocalAddr(), Remote: c.RemoteAddr()}
	}
	peer := &nmdcPeer{
		c: c, ip: ip,
		fea: fea,
	}
	peer.ext.userip2 = fea.Has(nmdcp.ExtUserIP2)
	peer.ext.botlist = fea.Has(nmdcp.ExtBotList)
	peer.ext.tths = fea.Has(nmdcp.ExtTTHS)
	h.newBasePeer(&peer.BasePeer, cinfo)
	peer.write.wake = make(chan struct{}, 1)
	peer.info.user.Name = nick
	return peer
}

type nmdcPeer struct {
	BasePeer

	c   *nmdc.Conn
	fea nmdcp.Extensions
	ip  net.IP

	write struct {
		wake chan struct{}
		cnt  uint32 // atomic
		sync.Mutex
		buf []nmdcp.Message
	}
	info struct {
		share uint64 // atomic
		sync.RWMutex
		user nmdcp.MyINFO
		buf  *bytes.Buffer
		raw  *nmdcp.RawMessage
	}
	ext struct {
		userip2 bool
		botlist bool
		tths    bool
	}

	search struct {
		sync.RWMutex
		peers  map[Peer]*nmdcSearchRun
		sorted []*nmdcSearchRun
	}
}

func (p *nmdcPeer) Searchable() bool {
	return atomic.LoadUint64(&p.info.share) > 0
}

type nmdcSearchRun struct {
	last int64 // sec
	req  SearchRequest
	out  Search
}

func (p *nmdcPeer) SetInfo(u *nmdcp.MyINFO) {
	p.info.Lock()
	defer p.info.Unlock()
	p.setUserInfo(u)
}

func (p *nmdcPeer) setUserInfo(u *nmdcp.MyINFO) {
	if u != &p.info.user {
		p.info.user = *u
	}
	if p.info.buf == nil {
		p.info.buf = bytes.NewBuffer(nil)
	} else {
		p.info.buf.Reset()
	}
	err := u.MarshalNMDC(p.c.TextEncoder(), p.info.buf)
	if err != nil {
		panic(err)
	}
	p.info.raw = &nmdcp.RawMessage{Typ: u.Type(), Data: p.info.buf.Bytes()}
	atomic.StoreUint64(&p.info.share, u.ShareSize)
}

func (p *nmdcPeer) UserInfo() UserInfo {
	u := p.Info()
	return UserInfo{
		Name:           string(u.Name),
		App:            u.Client,
		HubsNormal:     u.HubsNormal,
		HubsRegistered: u.HubsRegistered,
		HubsOperator:   u.HubsOperator,
		Slots:          u.Slots,
		Email:          u.Email,
		Desc:           u.Desc,
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

func (p *nmdcPeer) rawInfo() (*nmdcp.RawMessage, *encoding.Encoder) {
	p.info.RLock()
	data := p.info.raw
	p.info.RUnlock()
	return data, p.c.TextEncoder()
}

func (p *nmdcPeer) Info() nmdcp.MyINFO {
	p.info.RLock()
	u := p.info.user
	p.info.RUnlock()
	return u
}

func (p *nmdcPeer) closeOn(list []Peer) error {
	return p.closeWith(p,
		p.c.Close,
		func() error {
			p.hub.leave(p, p.sid, list)
			p.dropSearches()
			return nil
		},
	)
}

func (p *nmdcPeer) Close() error {
	return p.closeOn(nil)
}

func (p *nmdcPeer) writer(timeout time.Duration) {
	defer p.Close()
	ticker := time.NewTicker(time.Minute / 2)
	defer ticker.Stop()

	var buf2 []nmdcp.Message
	resetBuf := func(buf []nmdcp.Message) {
		for i := range buf {
			buf[i] = nil
		}
		buf2 = buf[:0]
	}
	logErr := func(err error) {
		if p.Online() {
			cntNMDCWriteErr.Add(1)
			log.Printf("%s: write: %v", p.c.RemoteAddr(), err)
		}
		return
	}
	var deadline time.Time
	for {
		select {
		case <-p.close.done:
			return
		case <-ticker.C:
			// keep alive
			_ = p.c.SetWriteDeadline(time.Now().Add(timeout))
			err := p.c.WriteLine([]byte("|"))
			if err != nil {
				logErr(err)
				return
			}
			err = p.c.Flush()
			if err != nil {
				logErr(err)
				return
			}
			_ = p.c.SetWriteDeadline(time.Time{})
		case <-p.write.wake:
			p.write.Lock()
			buf := p.write.buf
			p.write.buf = buf2
			atomic.StoreUint32(&p.write.cnt, 0)
			p.write.Unlock()
			numNMDCWriteQueue.Observe(float64(len(buf)))
			if len(buf) == 0 {
				resetBuf(buf)
				continue
			}
			start := time.Now()
			if start.After(deadline) || deadline.Sub(start) < (timeout*3)/4 {
				// set only if an old deadline is stale
				deadline = start.Add(timeout)
				_ = p.c.SetWriteDeadline(deadline)
			}
			err := p.c.WriteMsg(buf...)
			resetBuf(buf)
			if err != nil {
				durNMDCWrite.Observe(time.Since(start).Seconds())
				logErr(err)
				return
			}
			if atomic.LoadUint32(&p.write.cnt) > 0 && len(p.write.wake) != 0 {
				durNMDCWrite.Observe(time.Since(start).Seconds())
				continue // do not flush, continue batching
			}
			err = p.c.Flush()
			durNMDCWrite.Observe(time.Since(start).Seconds())
			if err != nil {
				logErr(err)
				return
			}
			_ = p.c.SetWriteDeadline(time.Time{})
			deadline = time.Time{}
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
	atomic.AddUint32(&p.write.cnt, 1)
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

func (p *nmdcPeer) Topic(topic string) error {
	return p.SendNMDC(&nmdcp.HubTopic{Text: topic})
}

func (p *nmdcPeer) PeersJoin(e *PeersJoinEvent) error {
	return p.peersJoin(e, false)
}

func (p *nmdcPeer) PeersUpdate(e *PeersUpdateEvent) error {
	// same as join
	return p.PeersJoin((*PeersJoinEvent)(e))
}

func NMDCUserInfo(p Peer) nmdcp.MyINFO {
	if np, ok := p.(*nmdcPeer); ok {
		return np.Info()
	}
	u := p.UserInfo()
	return u.toNMDC()
}

func (u UserInfo) toNMDC() nmdcp.MyINFO {
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
	conn := "100"                // TODO
	mode := nmdcp.UserModeActive // TODO
	if u.Kind == UserBot || u.Kind == UserHub {
		conn = "" // empty conn indicates a bot
	}
	return nmdcp.MyINFO{
		Name:           u.Name,
		Client:         u.App,
		HubsNormal:     u.HubsNormal,
		HubsRegistered: u.HubsRegistered,
		HubsOperator:   u.HubsOperator,
		Slots:          u.Slots,
		Email:          u.Email,
		Desc:           u.Desc,
		ShareSize:      u.Share,
		Flag:           flag,
		Conn:           conn,
		Mode:           mode,
	}
}

type nmdcRaw struct {
	input    []nmdcp.Message
	utf8     *nmdcRawEnc
	fallback *nmdcRawEnc
}

func (r *nmdcRaw) Encode(enc *encoding.Encoder, fnc func() []nmdcp.Message) ([]nmdcp.Message, error) {
	ptr := &r.utf8
	if enc != nil {
		// TODO: handle multiple encoding if necessary
		ptr = &r.fallback
	}
	raw := *ptr
	if raw != nil {
		return raw.cmds, raw.err
	}
	raw = &nmdcRawEnc{}
	*ptr = raw
	cmds := r.input
	if cmds == nil {
		cmds = fnc()
		r.input = cmds
	}
	err := raw.encode(enc, cmds)
	if err != nil {
		raw.err = err
		return nil, err
	}
	return raw.cmds, nil
}

type nmdcRawEnc struct {
	err  error
	cmds []nmdcp.Message
}

func (r *nmdcRawEnc) encode(enc *encoding.Encoder, m []nmdcp.Message) error {
	r.cmds = make([]nmdcp.Message, 0, len(m))
	buf := bytes.NewBuffer(nil)
	for _, m := range m {
		if raw, ok := m.(*nmdcp.RawMessage); ok {
			r.cmds = append(r.cmds, raw)
			continue
		}
		buf.Reset()
		if err := m.MarshalNMDC(enc, buf); err != nil {
			r.err = err
			return err
		}
		data := append([]byte{}, buf.Bytes()...)
		r.cmds = append(r.cmds, &nmdcp.RawMessage{Typ: m.Type(), Data: data})
	}
	return nil
}

func nmdcPeersJoinCmds(enc *encoding.Encoder, peers []Peer) []nmdcp.Message {
	cmds := make([]nmdcp.Message, 0, len(peers))
	for _, p2 := range peers {
		if p2n, ok := p2.(*nmdcPeer); ok {
			raw, enc2 := p2n.rawInfo()
			if (enc == nil && enc2 == nil) || (enc != nil && enc2 != nil) {
				// same encoding
				cmds = append(cmds, raw)
			} else {
				myinfo := p2n.Info()
				cmds = append(cmds, &myinfo)
			}
		} else {
			myinfo := p2.UserInfo().toNMDC()
			cmds = append(cmds, &myinfo)
		}
	}
	return cmds
}

func nmdcPeersOpCmds(peers []Peer) []nmdcp.Message {
	var ops nmdcp.Names
	for _, p2 := range peers {
		if p2.User().Has(FlagOpIcon) {
			// operator flag is sent as a separate command
			ops = append(ops, p2.Name())
		}
	}
	if len(ops) == 0 {
		return nil
	}
	return []nmdcp.Message{&nmdcp.OpList{Names: ops}}
}

func nmdcPeersBotsCmds(peers []Peer) []nmdcp.Message {
	var bots nmdcp.Names
	for _, p2 := range peers {
		if info := p2.UserInfo(); info.Kind == UserBot || info.Kind == UserHub {
			bots = append(bots, p2.Name())
		}
	}
	if len(bots) == 0 {
		return nil
	}
	return []nmdcp.Message{&nmdcp.BotList{Names: bots}}
}

func nmdcPeersIPCmds(peers []Peer) []nmdcp.Message {
	var ips []nmdcp.UserAddress
	for _, p2 := range peers {
		if addr, ok := p2.RemoteAddr().(*net.TCPAddr); ok {
			ips = append(ips, nmdcp.UserAddress{
				Name: p2.Name(),
				IP:   addr.IP.String(),
			})
		}
	}
	if len(ips) == 0 {
		return nil
	}
	return []nmdcp.Message{&nmdcp.UserIP{List: ips}}
}

func (p *nmdcPeer) peersJoin(e *PeersJoinEvent, initial bool) error {
	enc := p.c.TextEncoder()

	cmds, err := e.nmdcInfos.Encode(enc, func() []nmdcp.Message {
		return nmdcPeersJoinCmds(enc, e.Peers)
	})
	if err != nil {
		return err
	}
	if initial {
		// will send ips, ops and bots manually
		return p.c.WriteMsg(cmds...)
	}
	cmds = cmds[:len(cmds):len(cmds)] // realloc on append

	// operators flag is a separate command
	opsCmd, err := e.nmdcOps.Encode(enc, func() []nmdcp.Message {
		return nmdcPeersOpCmds(e.Peers)
	})
	if err != nil {
		return err
	}
	cmds = append(cmds, opsCmd...)

	// if supported, send a bot list
	if p.ext.botlist {
		botsCmd, err := e.nmdcBots.Encode(enc, func() []nmdcp.Message {
			return nmdcPeersBotsCmds(e.Peers)
		})
		if err != nil {
			return err
		}
		cmds = append(cmds, botsCmd...)
	}

	// send IPs if the user is an operator
	if p.ext.userip2 && p.User().HasPerm(PermIP) {
		ipsCmd, err := e.nmdcIPs.Encode(enc, func() []nmdcp.Message {
			return nmdcPeersIPCmds(e.Peers)
		})
		if err != nil {
			return err
		}
		cmds = append(cmds, ipsCmd...)
	}
	return p.SendNMDC(cmds...)
}

func nmdcPeersLeaveCmds(peers []Peer) []nmdcp.Message {
	cmds := make([]nmdcp.Message, 0, len(peers))
	for _, p2 := range peers {
		cmds = append(cmds, &nmdcp.Quit{
			Name: nmdcp.Name(p2.Name()),
		})
	}
	return cmds
}

func (p *nmdcPeer) PeersLeave(e *PeersLeaveEvent) error {
	if e == nil || len(e.Peers) == 0 {
		return nil
	} else if !p.Online() {
		return errConnectionClosed
	}
	enc := p.c.TextEncoder()
	cmds, err := e.nmdcQuit.Encode(enc, func() []nmdcp.Message {
		return nmdcPeersLeaveCmds(e.Peers)
	})
	if err != nil {
		return err
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
			Client:     p.hub.getSoft(),
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
			Text: "/me joined",
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
			Text: "/me parted",
		},
		&nmdcp.Quit{
			Name: nmdcp.Name(rname),
		},
	)
}

func ToNMDCChatMsg(from Peer, msg Message) *nmdcp.ChatMessage {
	if msg.Me && !strings.HasPrefix(msg.Text, "/me") {
		msg.Text = "/me " + msg.Text
	}
	if msg.Name == "" {
		msg.Name = from.Name()
	}
	return &nmdcp.ChatMessage{
		Name: msg.Name,
		Text: msg.Text,
	}
}

func (p *nmdcPeer) ChatMsg(room *Room, from Peer, msg Message) error {
	if !p.Online() {
		return errConnectionClosed
	}
	m := ToNMDCChatMsg(from, msg)
	if room == nil || room.Name() == "" {
		return p.SendNMDC(m)
	}
	if from == p {
		return nil // no echo
	}
	return p.SendNMDC(&nmdcp.PrivateMessage{
		From: room.Name(),
		To:   p.Name(),
		Name: m.Name,
		Text: m.Text,
	})
}

func (p *nmdcPeer) PrivateMsg(from Peer, msg Message) error {
	if !p.Online() {
		return errConnectionClosed
	}
	if msg.Me && !strings.HasPrefix(msg.Text, "/me") {
		msg.Text = "/me " + msg.Text
	}
	fname := msg.Name
	return p.SendNMDC(&nmdcp.PrivateMessage{
		From: fname, Name: fname,
		To:   p.Name(),
		Text: msg.Text,
	})
}

func (p *nmdcPeer) HubChatMsg(m Message) error {
	if !p.Online() {
		return errConnectionClosed
	}
	if m.Name == "" {
		m.Name = p.hub.getName()
	}
	if m.Me && !strings.HasPrefix(m.Text, "/me") {
		m.Text = "/me " + m.Text
	}
	return p.SendNMDC(&nmdcp.ChatMessage{Name: m.Name, Text: m.Text})
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

	rawSP     nmdcRaw
	rawSearch nmdcRaw
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

func (p *nmdcPeer) gcSearches() {
	now := time.Now().Unix()
	last := -1
	for i, s := range p.search.sorted {
		if now-atomic.LoadInt64(&s.last) > int64(searchTimeout/time.Second) {
			last = i
		} else {
			break
		}
	}
	if last < 1000 && last < len(p.search.sorted)/40 {
		return
	}
	for _, s := range p.search.sorted[:last] {
		_ = s.out.Close()
		delete(p.search.peers, s.out.Peer())
	}
	p.search.sorted = p.search.sorted[last:]
}

func (p *nmdcPeer) dropSearches() {
	p.search.Lock()
	p.search.peers = nil
	p.search.Unlock()
}

func (p *nmdcPeer) setActiveSearch(out Search, req SearchRequest) {
	p2 := out.Peer()
	p.search.Lock()
	defer p.search.Unlock()
	cur := p.search.peers[p2]
	if cur != nil && cur.out != nil {
		_ = cur.out.Close()
	}
	if p.search.peers == nil {
		p.search.peers = make(map[Peer]*nmdcSearchRun)
	} else {
		p.gcSearches()
	}
	s := &nmdcSearchRun{out: out, req: req}
	atomic.StoreInt64(&s.last, time.Now().Unix())
	p.search.peers[p2] = s
	p.search.sorted = append(p.search.sorted, s)
}

func (p *nmdcPeer) searchCmdTTH(from Peer, req TTH) nmdcp.Message {
	if p.ext.tths {
		return &nmdcp.TTHSearchPassive{
			User: from.Name(),
			TTH:  TTH(req),
		}
	}
	return &nmdcp.Search{
		User:     from.Name(),
		DataType: nmdcp.DataTypeTTH, TTH: (*TTH)(&req),
	}
}

func (p *nmdcPeer) searchCmdOther(from Peer, req SearchRequest) *nmdcp.Search {
	msg := &nmdcp.Search{
		User:     from.Name(),
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
	return msg
}

func (p *nmdcPeer) Search(ctx context.Context, req SearchRequest, out Search) error {
	if !p.Online() {
		return errConnectionClosed
	}
	p.setActiveSearch(out, req)
	if req, ok := req.(TTHSearch); ok {
		if ns, ok := out.(*nmdcSearch); ok {
			enc := ns.p.c.TextEncoder()
			raw := &ns.rawSearch
			if p.ext.tths {
				raw = &ns.rawSP
			}
			cmds, err := raw.Encode(enc, func() []nmdcp.Message {
				return []nmdcp.Message{p.searchCmdTTH(out.Peer(), TTH(req))}
			})
			if err != nil {
				return err
			}
			return p.SendNMDC(cmds...)
		}
		cmd := p.searchCmdTTH(out.Peer(), TTH(req))
		return p.SendNMDC(cmd)
	}
	if ns, ok := out.(*nmdcSearch); ok {
		enc := ns.p.c.TextEncoder()
		cmds, err := ns.rawSearch.Encode(enc, func() []nmdcp.Message {
			return []nmdcp.Message{p.searchCmdOther(out.Peer(), req)}
		})
		if err != nil {
			return err
		}
		return p.SendNMDC(cmds...)
	}
	msg := p.searchCmdOther(out.Peer(), req)
	return p.SendNMDC(msg)
}

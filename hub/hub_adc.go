package hub

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	dc "github.com/direct-connect/go-dc"
	"github.com/direct-connect/go-dc/tiger"
	"github.com/direct-connect/go-dcpp/adc"
	"github.com/direct-connect/go-dcpp/adc/types"
)

func (h *Hub) initADC() {
	h.peers.loggingCID = make(map[adc.CID]struct{})
	h.peers.byCID = make(map[adc.CID]*adcPeer)
}

func (h *Hub) ServeADC(conn net.Conn) error {
	cntConnADC.Add(1)
	cntConnADCOpen.Add(1)
	defer cntConnADCOpen.Add(-1)

	log.Printf("%s: using ADC", conn.RemoteAddr())
	c, err := adc.NewConn(conn)
	if err != nil {
		return err
	}
	defer c.Close()
	c.SetWriteTimeout(writeTimeout)
	c.OnLineR(func(line []byte) (bool, error) {
		sizeADCLinesR.Observe(float64(len(line)))
		return true, nil
	})
	c.OnLineW(func(line []byte) (bool, error) {
		sizeADCLinesW.Observe(float64(len(line)))
		return true, nil
	})

	peer, err := h.adcHandshake(c)
	if err != nil {
		return err
	}
	defer peer.Close()
	return h.adcServePeer(peer)
}

func (h *Hub) adcHandshake(c *adc.Conn) (*adcPeer, error) {
	defer measure(durADCHandshake)()

	peer, err := h.adcStageProtocol(c)
	if err != nil {
		return nil, err
	}
	// connection is not yet valid and we haven't added the client to the hub yet
	if err := h.adcStageIdentity(peer); err != nil {
		return nil, err
	}
	// TODO: identify pingers
	// peer registered, now we can start serving things
	if err = h.sendMOTD(peer); err != nil {
		_ = peer.Close()
		return nil, err
	}
	if h.conf.ChatLogJoin != 0 {
		h.globalChat.ReplayChat(peer, h.conf.ChatLogJoin)
	}
	return peer, nil
}

func (h *Hub) adcServePeer(peer *adcPeer) error {
	peer.conn.KeepAlive(time.Minute / 2)
	if !h.callOnJoined(peer) {
		return nil // TODO: eny errors?
	}
	for {
		p, err := peer.conn.ReadPacket(time.Time{})
		if err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}
		if err = h.adcHandlePacket(peer, p); err != nil {
			return err
		}
	}
}

func (h *Hub) adcHandlePacket(peer *adcPeer, p adc.Packet) error {
	skind := string(p.Kind())
	cntADCPackets.WithLabelValues(skind).Add(1)
	defer measure(durADCHandlePacket.WithLabelValues(skind))()

	cmd := p.Message().Type.String()
	cntADCCommands.WithLabelValues(cmd).Add(1)
	defer measure(durADCHandleCommand.WithLabelValues(cmd))()

	switch p := p.(type) {
	case *adc.BroadcastPacket:
		if peer.sid != p.ID {
			return errors.New("malformed broadcast")
		}
		h.adcBroadcast(p, peer)
		return nil
	case *adc.FeaturePacket:
		if peer.sid != p.ID {
			return errors.New("malformed features broadcast")
		}
		// TODO: we ignore feature selectors for now
		h.adcBroadcast(&adc.BroadcastPacket{BasePacket: p.BasePacket, ID: p.ID}, peer)
		return nil
	case *adc.EchoPacket:
		if peer.sid != p.ID {
			return errors.New("malformed echo packet")
		}
		if err := peer.conn.WritePacket(p); err != nil {
			return err
		}
		if err := peer.conn.Flush(); err != nil {
			return err
		}
		h.adcDirect((*adc.DirectPacket)(p), peer)
		return nil
	case *adc.DirectPacket:
		if peer.sid != p.ID {
			return errors.New("malformed direct packet")
		}
		h.adcDirect(p, peer)
		return nil
	case *adc.HubPacket:
		h.adcHub(p, peer)
		return nil
	case *adc.ClientPacket, *adc.UDPPacket:
		return errors.New("invalid packet kind")
	default:
		data, _ := p.MarshalPacket()
		log.Printf("%s: adc: %s", peer.RemoteAddr(), string(data))
		return nil
	}
}

func (h *Hub) adcStageProtocol(c *adc.Conn) (*adcPeer, error) {
	deadline := time.Now().Add(time.Second * 5)
	// Expect features from the client
	p, err := c.ReadPacket(deadline)
	if err != nil {
		return nil, err
	}
	hp, ok := p.(*adc.HubPacket)
	if !ok {
		return nil, fmt.Errorf("expected hub messagge, got: %#v", p)
	} else if hp.Name != (adc.Supported{}).Cmd() {
		return nil, fmt.Errorf("expected support message, got: %v", hp.Name)
	}
	var sup adc.Supported
	if err := adc.Unmarshal(hp.Data, &sup); err != nil {
		return nil, err
	}
	for ext, on := range sup.Features {
		if !on {
			continue
		}
		cntADCExtensions.WithLabelValues(ext.String()).Add(1)
	}
	hubFeatures := adc.ModFeatures{
		// should always be set for ADC
		adc.FeaBASE: true,
		adc.FeaBAS0: true,
		adc.FeaTIGR: true,
		// extensions
		adc.FeaPING: true,
		adc.FeaUCMD: true,
		adc.FeaUCM0: true,
	}

	mutual := hubFeatures.Intersect(sup.Features)
	if !mutual.IsSet(adc.FeaBASE) && !mutual.IsSet(adc.FeaBAS0) {
		return nil, fmt.Errorf("client does not support BASE")
	} else if !mutual.IsSet(adc.FeaTIGR) {
		return nil, fmt.Errorf("client does not support TIGR")
	}

	// send features supported by the hub
	err = c.WriteInfoMsg(adc.Supported{
		Features: hubFeatures,
	})
	if err != nil {
		return nil, err
	}
	// and allocate a SID for the client
	sid := h.nextSID()
	err = c.WriteInfoMsg(adc.SIDAssign{
		SID: sid,
	})
	if err != nil {
		return nil, err
	}
	err = c.Flush()
	if err != nil {
		return nil, err
	}
	return &adcPeer{
		BasePeer: BasePeer{
			hub:      h,
			hubAddr:  c.LocalAddr(),
			peerAddr: c.RemoteAddr(),
			sid:      sid,
		},
		conn: c,
		fea:  mutual,
	}, nil
}

func (h *Hub) adcStageIdentity(peer *adcPeer) error {
	deadline := time.Now().Add(time.Second * 5)
	// client should send INF with ID and PID set
	p, err := peer.conn.ReadPacket(deadline)
	if err != nil {
		return err
	}
	b, ok := p.(*adc.BroadcastPacket)
	if !ok {
		return fmt.Errorf("expected user info broadcast, got %#v", p)
	} else if b.Name != (adc.User{}).Cmd() {
		return fmt.Errorf("expected user info message, got %v", b.Name)
	}
	var u adc.User
	if err := adc.Unmarshal(b.Data, &u); err != nil {
		return err
	}
	for _, ext := range u.Features {
		cntADCExtensions.WithLabelValues(ext.String()).Add(1)
	}
	if u.Id != u.Pid.Hash() {
		err = errors.New("invalid pid supplied")
		_ = peer.sendError(adc.Fatal, 27, err)
		return err
	}
	u.Pid = nil

	err = h.validateUserName(u.Name)
	if err != nil {
		_ = peer.sendError(adc.Fatal, 21, err)
		return err
	}

	// do not lock for writes first
	h.peers.RLock()
	_, sameName1 := h.peers.reserved[u.Name]
	_, sameName2 := h.peers.byName[u.Name]
	_, sameCID1 := h.peers.loggingCID[u.Id]
	_, sameCID2 := h.peers.byCID[u.Id]
	h.peers.RUnlock()

	if sameName1 || sameName2 {
		err = errNickTaken
		_ = peer.sendError(adc.Fatal, 22, err)
		return err
	}
	if sameCID1 || sameCID2 {
		err = errors.New("CID taken")
		_ = peer.sendError(adc.Fatal, 24, err)
		return err
	}

	// ok, now lock for writes and try to bind nick and CID
	h.peers.Lock()
	_, sameName1 = h.peers.reserved[u.Name]
	_, sameName2 = h.peers.byName[u.Name]
	if sameName1 || sameName2 {
		h.peers.Unlock()

		err = errNickTaken
		_ = peer.sendError(adc.Fatal, 22, err)
		return err
	}
	_, sameCID1 = h.peers.loggingCID[u.Id]
	_, sameCID2 = h.peers.byCID[u.Id]
	if sameCID1 || sameCID2 {
		h.peers.Unlock()

		err = errors.New("CID taken")
		_ = peer.sendError(adc.Fatal, 24, err)
		return err
	}
	// bind nick and cid, still no one will see us yet
	h.peers.reserved[u.Name] = struct{}{}
	h.peers.loggingCID[u.Id] = struct{}{}
	h.peers.Unlock()

	unbind := func() {
		h.peers.Lock()
		delete(h.peers.reserved, u.Name)
		delete(h.peers.loggingCID, u.Id)
		h.peers.Unlock()
	}

	if u.Ip4 == "0.0.0.0" {
		if t, ok := peer.peerAddr.(*net.TCPAddr); ok {
			if ip4 := t.IP.To4(); ip4 != nil {
				u.Ip4 = ip4.String()
			}
		}
	}
	if u.Ip6 == "::" {
		if t, ok := peer.peerAddr.(*net.TCPAddr); ok {
			if ip6 := t.IP.To16(); ip6 != nil {
				u.Ip6 = ip6.String()
			}
		}
	}
	u.Normalize()
	peer.user = u

	st := h.Stats()
	if err := h.adcStageVerify(peer); err != nil {
		unbind()
		return err
	}
	deadline = time.Now().Add(time.Second * 5)

	// send hub info
	err = peer.conn.WriteInfoMsg(adc.HubInfo{
		Name:        st.Name,
		Desc:        st.Desc,
		Application: st.Soft.Name,
		Version:     st.Soft.Version,
		Address:     st.DefaultAddr(),
		Users:       st.Users,
	})
	if err != nil {
		unbind()
		return err
	}
	// send OK status
	err = peer.conn.WriteInfoMsg(adc.Status{
		Sev:  adc.Success,
		Code: 0,
		Msg:  "powered by Gophers",
	})

	// send user list (except his own info)
	err = peer.PeersJoin(h.Peers())
	if err != nil {
		unbind()
		return err
	}

	// write his info and flush
	err = peer.PeersJoin([]Peer{peer})
	if err != nil {
		unbind()
		return err
	}

	if peer.fea.IsSet(adc.FeaUCMD) || peer.fea.IsSet(adc.FeaUCM0) {
		err = h.adcSendUserCommand(peer)
		if err != nil {
			unbind()
			return err
		}
	}

	// finally accept the user on the hub
	h.peers.Lock()
	// cleanup temporary bindings
	delete(h.peers.reserved, peer.user.Name)
	delete(h.peers.loggingCID, u.Id)

	// make a snapshot of peers to send info to
	list := h.listPeers()

	// add user to the hub
	h.peers.bySID[peer.sid] = peer
	h.peers.byCID[u.Id] = peer
	h.peers.byName[u.Name] = peer
	h.invalidateList()
	h.globalChat.Join(peer)
	cntPeers.Add(1)
	h.peers.Unlock()

	// notify other users about the new one
	h.broadcastUserJoin(peer, list)
	return nil
}

func (h *Hub) adcStageVerify(peer *adcPeer) error {
	isRegistered, err := h.IsRegistered(peer.Name())
	if err != nil {
		return err
	} else if !isRegistered {
		return nil
	}
	// give the user a minute to enter a password
	deadline := time.Now().Add(time.Minute)
	//some bytes for check password
	var salt [24]byte
	rand.Read(salt[:])
	err = peer.conn.WriteInfoMsg(adc.GetPassword{
		Salt: salt[:],
	})
	if err != nil {
		return err
	}
	err = peer.conn.Flush()
	if err != nil {
		return err
	}

	p, err := peer.conn.ReadPacket(deadline)
	if err != nil {
		return err
	}
	hp, ok := p.(*adc.HubPacket)
	if !ok {
		return fmt.Errorf("expected hub messagge, got: %#v", p)
	} else if hp.Name != (adc.Password{}).Cmd() {
		return fmt.Errorf("expected user password message, got %v", hp.Name)
	}
	var pass adc.Password
	if err := adc.Unmarshal(hp.Data, &pass); err != nil {
		return err
	}
	ok, err = h.adcCheckUserPass(peer.Name(), salt[:], pass.Hash)
	if err != nil {
		return err
	} else if !ok {
		err = errors.New("wrong password")
		_ = peer.sendError(adc.Fatal, 23, err)
		return err
	}
	return nil
}

func (h *Hub) adcCheckUserPass(name string, salt []byte, hash tiger.Hash) (bool, error) {
	if h.userDB == nil {
		return false, nil
	}
	pass, err := h.userDB.GetUserPassword(name)
	if err != nil {
		return false, err
	}
	check := make([]byte, len(pass)+len(salt))
	i := copy(check, pass)
	copy(check[i:], salt)
	exp := tiger.HashBytes(check)
	return exp == hash, nil
}

func (h *Hub) adcHub(p *adc.HubPacket, from Peer) {
	// TODO: disallow INF, STA and some others
	msg, err := p.Decode()
	if err != nil {
		log.Printf("cannot parse ADC message: %v", err)
		return
	}
	switch msg := msg.(type) {
	case adc.ChatMessage:
		text := string(msg.Text)
		if h.isCommand(from, text) {
			return
		}
		// ignore
	default:
		// TODO: decode other packets
	}
}

func (h *Hub) adcSendUserCommand(peer *adcPeer) error {
	for _, c := range h.ListCommands() {
		err := peer.conn.WriteInfoMsg(adc.UserCommand{
			Path:     c.Menu,
			Command:  "HMSG !" + c.Name + "\n",
			Category: adc.CategoryUser,
		})
		if err != nil {
			return err
		}
	}
	return peer.conn.Flush()
}

func (h *Hub) adcBroadcast(p *adc.BroadcastPacket, from *adcPeer) {
	msg, err := p.Decode()
	if err != nil {
		log.Printf("cannot parse ADC message: %v", err)
		return
	}
	// TODO: read INF, update peer info
	// TODO: update nick, make sure there is no duplicates
	// TODO: disallow STA and some others
	switch msg := msg.(type) {
	case adc.ChatMessage:
		text := string(msg.Text)
		if h.isCommand(from, text) {
			return
		}
		h.globalChat.SendChat(from, text)
	case adc.SearchRequest:
		h.adcHandleSearch(from, &msg, nil)
	default:
		// TODO: decode other packets
		for _, peer := range h.Peers() {
			if p2, ok := peer.(*adcPeer); ok {
				_ = p2.conn.WritePacket(p)
				_ = p2.conn.Flush()
			}
		}
	}
}

func (h *Hub) adcDirect(p *adc.DirectPacket, from *adcPeer) {
	// TODO: disallow INF, STA and some others
	peer := h.bySID(p.Targ)
	if peer == nil {
		r := h.roomBySID(p.Targ)
		if r == nil {
			return
		}
		msg, err := p.Decode()
		if err != nil {
			log.Printf("cannot parse ADC message: %v", err)
			return
		}
		switch msg := msg.(type) {
		case adc.ChatMessage:
			r.SendChat(from, string(msg.Text))
		}
		return
	}
	msg, err := p.Decode()
	if err != nil {
		log.Printf("cannot parse ADC message: %v", err)
		return
	}
	switch msg := msg.(type) {
	case adc.ChatMessage:
		h.privateChat(from, peer, Message{
			Name: from.Name(),
			Text: string(msg.Text),
		})
	case adc.ConnectRequest:
		info := from.Info()
		pinf := peer.User()
		var ip string
		if pinf.IPv6 && info.Ip6 != "" {
			ip = info.Ip6
		} else if pinf.IPv4 {
			ip = info.Ip4
		}
		if ip == "" {
			return
		}
		secure := strings.HasPrefix(msg.Proto, "ADCS")
		h.connectReq(from, peer, ip+":"+strconv.Itoa(msg.Port), msg.Token, secure)
	case adc.RevConnectRequest:
		secure := strings.HasPrefix(msg.Proto, "ADCS")
		h.revConnectReq(from, peer, msg.Token, secure)
	case adc.SearchRequest:
		h.adcHandleSearch(from, &msg, []Peer{peer})
	case adc.SearchResult:
		h.adcHandleResult(from, peer, &msg)
	default:
		// TODO: decode other packets
		if p2, ok := peer.(*adcPeer); ok {
			_ = p2.conn.WritePacket(p)
			_ = p2.conn.Flush()
		}
	}
}

func (h *Hub) adcHandleSearch(peer *adcPeer, req *adc.SearchRequest, peers []Peer) {
	s := peer.newSearch(req.Token)
	if req.TTH != nil {
		// ignore other parameters
		h.Search(TTHSearch(*req.TTH), s, peers)
		return
	}
	name := NameSearch{
		And: req.And,
		Not: req.Not,
	}
	var sr SearchRequest = name
	if req.Type == adc.FileTypeFile ||
		req.Eq != 0 || req.Le != 0 || req.Ge != 0 ||
		len(req.Ext) != 0 || len(req.NoExt) != 0 ||
		req.Group != adc.ExtNone {
		freq := FileSearch{
			NameSearch: name,
			MinSize:    uint64(req.Ge),
			MaxSize:    uint64(req.Le),
			Ext:        req.Ext,
			NoExt:      req.NoExt,
		}
		if req.Eq != 0 {
			freq.MinSize = uint64(req.Eq)
			freq.MaxSize = uint64(req.Eq)
		}
		switch req.Group {
		case adc.ExtAudio:
			freq.FileType = FileTypeAudio
		case adc.ExtArch:
			freq.FileType = FileTypeCompressed
		case adc.ExtDoc:
			freq.FileType = FileTypeDocuments
		case adc.ExtExe:
			freq.FileType = FileTypeExecutable
		case adc.ExtImage:
			freq.FileType = FileTypePicture
		case adc.ExtVideo:
			freq.FileType = FileTypeVideo
		}
		sr = freq
	}
	h.Search(sr, s, peers)
}

func (h *Hub) adcHandleResult(peer *adcPeer, to Peer, res *adc.SearchResult) {
	if to, ok := to.(*adcPeer); ok {
		_ = to.conn.WriteDirect(peer.SID(), to.SID(), *res)
		_ = to.conn.Flush()
		return
	}
	peer.search.RLock()
	s := peer.search.tokens[res.Token]
	peer.search.RUnlock()
	if s == nil {
		return
	}
	res.Path = strings.TrimPrefix(res.Path, "/")
	var sr SearchResult
	if res.TTH != nil {
		sr = File{Peer: peer, Path: res.Path, Size: uint64(res.Size), TTH: res.TTH}
	} else {
		sr = Dir{Peer: peer, Path: res.Path}
	}
	if err := s.SendResult(sr); err != nil {
		_ = s.Close()
		peer.search.Lock()
		delete(peer.search.tokens, res.Token)
		peer.search.Unlock()
	}
}

var _ Peer = (*adcPeer)(nil)

type adcPeer struct {
	BasePeer

	conn *adc.Conn
	fea  adc.ModFeatures

	mu   sync.RWMutex
	user adc.User

	closeMu sync.Mutex
	closed  bool

	search struct {
		sync.RWMutex
		tokens map[string]Search
	}
}

func (p *adcPeer) Name() string {
	p.mu.RLock()
	name := p.user.Name
	p.mu.RUnlock()
	return name
}

func (p *adcPeer) Info() adc.User {
	p.mu.RLock()
	u := p.user
	p.mu.RUnlock()
	return u
}

func (p *adcPeer) User() User {
	u := p.Info()
	return User{
		Name:  u.Name,
		Share: uint64(u.ShareSize),
		Email: u.Email,
		App: dc.Software{
			Name:    u.Application,
			Version: u.Version,
		},
		HubsNormal:     u.HubsNormal,
		HubsRegistered: u.HubsRegistered,
		HubsOperator:   u.HubsOperator,
		Slots:          u.Slots,
		IPv4:           u.Features.Has(adc.FeaTCP4),
		IPv6:           u.Features.Has(adc.FeaTCP6),
		TLS:            u.Features.Has(adc.FeaADC0),
	}
}

func (p *adcPeer) sendInfo(m adc.Message) error {
	err := p.conn.WriteInfoMsg(m)
	if err != nil {
		return err
	}
	return p.conn.Flush()
}

func (p *adcPeer) sendError(sev adc.Severity, code int, err error) error {
	return p.sendInfo(adc.Status{
		Sev: sev, Code: code, Msg: err.Error(),
	})
}

func (p *adcPeer) Close() error {
	p.closeMu.Lock()
	defer p.closeMu.Unlock()
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed {
		return nil
	}
	err := p.conn.Close()
	p.closed = true

	p.hub.leaveCID(p, p.sid, p.user.Id, p.user.Name)
	return err
}

func (p *adcPeer) BroadcastJoin(peers []Peer) {
	for _, p2 := range peers {
		_ = p2.PeersJoin([]Peer{p})
	}
}

func (p *adcPeer) PeersJoin(peers []Peer) error {
	for _, peer := range peers {
		var u adc.User
		if p2, ok := peer.(*adcPeer); ok {
			u = p2.Info()
		} else {
			// TODO: same address from multiple clients behind NAT, so we addend the name
			addr, _, _ := net.SplitHostPort(peer.RemoteAddr().String())
			info := peer.User()
			// TODO: once we support name changes, we should make the user
			//       virtually leave and rejoin with a new CID
			cid := adc.CID(tiger.HashBytes([]byte(info.Name + "\x00" + addr)))
			u = adc.User{
				Name:           info.Name,
				Id:             cid,
				Application:    info.App.Name,
				Version:        info.App.Version,
				HubsNormal:     info.HubsNormal,
				HubsRegistered: info.HubsRegistered,
				HubsOperator:   info.HubsOperator,
				Slots:          info.Slots,
				ShareSize:      int64(info.Share),
				Email:          info.Email,
			}
			if info.TLS {
				u.Features = append(u.Features, adc.FeaADC0)
			}
			if info.IPv4 {
				u.Features = append(u.Features, adc.FeaTCP4)
			}
			if info.IPv6 {
				u.Features = append(u.Features, adc.FeaTCP6)
			}
			if t, ok := peer.RemoteAddr().(*net.TCPAddr); ok {
				if ip4 := t.IP.To4(); ip4 != nil {
					u.Ip4 = ip4.String()
				} else {
					u.Ip6 = t.IP.String()
				}
			}
		}
		if u.Application != "" && p.user.Application == "" {
			// doesn't support AP field
			u.Application, u.Version = "", u.Application+" "+u.Version
		}
		if err := p.conn.WriteBroadcast(peer.SID(), &u); err != nil {
			return err
		}
	}
	return p.conn.Flush()
}

func (p *adcPeer) BroadcastLeave(peers []Peer) {
	for _, p2 := range peers {
		_ = p2.PeersLeave([]Peer{p})
	}
}

func (p *adcPeer) PeersLeave(peers []Peer) error {
	for _, peer := range peers {
		if err := p.conn.WriteInfoMsg(&adc.Disconnect{
			ID: peer.SID(),
		}); err != nil {
			return err
		}
	}
	return p.conn.Flush()
}

func (p *adcPeer) JoinRoom(room *Room) error {
	if room.Name() == "" {
		return nil
	}
	rsid := room.SID()
	rname := room.Name()
	h := tiger.HashBytes([]byte(rname)) // TODO: include hub name?
	err := p.conn.WriteBroadcast(rsid, adc.User{
		Id:          types.CID(h),
		Name:        rname,
		HubsNormal:  room.Users(), // TODO: update
		Application: p.hub.conf.Soft.Name,
		Version:     p.hub.conf.Soft.Version,
		Type:        adc.UserTypeOperator,
		Slots:       1,
	})
	if err != nil {
		return err
	}
	err = p.conn.WriteDirect(rsid, p.SID(), adc.ChatMessage{
		Text: "joined the room", PM: &rsid,
	})
	if err != nil {
		return err
	}
	return p.conn.Flush()
}

func (p *adcPeer) LeaveRoom(room *Room) error {
	if room.Name() == "" {
		return nil
	}
	rsid := room.SID()
	err := p.conn.WriteDirect(rsid, p.SID(), adc.ChatMessage{
		Text: "left the room", PM: &rsid,
	})
	if err != nil {
		return err
	}
	err = p.conn.WriteBroadcast(rsid, adc.Disconnect{
		ID: rsid,
	})
	if err != nil {
		return err
	}
	return p.conn.Flush()
}

func (p *adcPeer) ChatMsg(room *Room, from Peer, msg Message) error {
	var err error
	if room.Name() != "" {
		if p == from {
			return nil // no echo
		}
		rsid := room.SID()
		fsid := from.SID()
		err = p.conn.WriteDirect(fsid, p.SID(), adc.ChatMessage{
			Text: msg.Text, PM: &rsid,
		})
	} else {
		err = p.conn.WriteBroadcast(from.SID(), &adc.ChatMessage{
			Text: msg.Text,
		})
	}
	if err != nil {
		return err
	}
	return p.conn.Flush()
}

func (p *adcPeer) PrivateMsg(from Peer, msg Message) error {
	src := from.SID()
	err := p.conn.WriteDirect(src, p.sid, &adc.ChatMessage{
		Text: msg.Text, PM: &src,
	})
	if err != nil {
		return err
	}
	return p.conn.Flush()
}

func (p *adcPeer) HubChatMsg(text string) error {
	err := p.conn.WriteInfoMsg(&adc.ChatMessage{
		Text: text,
	})
	if err != nil {
		return err
	}
	return p.conn.Flush()
}

func (p *adcPeer) ConnectTo(peer Peer, addr string, token string, secure bool) error {
	host, sport, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	port, err := strconv.Atoi(sport)
	if err != nil {
		return err
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("invalid ip address: %q", host)
	}

	var field [2]byte
	// make sure we are on the same page - fake an update of an address for that peer
	if ip4 := ip.To4(); ip4 != nil {
		field = [2]byte{'I', '4'} // IPv4
	} else {
		field = [2]byte{'I', '6'} // IPv6
	}
	err = p.conn.WriteInfoMsg(adc.UserMod{
		field: host,
	})
	if err != nil {
		return err
	}

	// we need to pretend that peer speaks the same protocol as we do
	proto := adc.ProtoADC
	if secure {
		proto = adc.ProtoADCS
	}
	err = p.conn.WriteDirect(peer.SID(), p.sid, &adc.ConnectRequest{
		Proto: proto,
		Port:  port,
		Token: token,
	})
	if err != nil {
		return err
	}

	return p.conn.Flush()
}

func (p *adcPeer) RevConnectTo(peer Peer, token string, secure bool) error {
	// we need to pretend that peer speaks the same protocol as we do
	proto := adc.ProtoADC
	if secure {
		proto = adc.ProtoADCS
	}
	err := p.conn.WriteDirect(peer.SID(), p.sid, &adc.RevConnectRequest{
		Proto: proto,
		Token: token,
	})
	if err != nil {
		return err
	}
	return p.conn.Flush()
}

func (p *adcPeer) newSearch(token string) Search {
	return &adcSearch{p: p, token: token}
}

type adcSearch struct {
	p     *adcPeer
	token string
}

func (s *adcSearch) Peer() Peer {
	return s.p
}

func (s *adcSearch) SendResult(r SearchResult) error {
	sr := adc.SearchResult{
		Token: s.token,
		Slots: 1, // TODO
	}
	switch r := r.(type) {
	case Dir:
		if !strings.HasPrefix(r.Path, "/") {
			r.Path = "/" + r.Path
		}
		sr.Path = r.Path
	case File:
		if !strings.HasPrefix(r.Path, "/") {
			r.Path = "/" + r.Path
		}
		sr.Path = r.Path
		sr.Size = int64(r.Size)
		sr.TTH = r.TTH
	default:
		return nil // ignore
	}
	err := s.p.conn.WriteDirect(r.From().SID(), s.p.SID(), sr)
	if err == nil {
		err = s.p.conn.Flush()
	}
	return err
}

func (s *adcSearch) Close() error {
	return nil // TODO: block new results
}

func (p *adcPeer) searchToken(out Search) string {
	token := strconv.FormatUint(rand.Uint64(), 16)
	p.search.Lock()
	if p.search.tokens == nil {
		p.search.tokens = make(map[string]Search)
	}
	p.search.tokens[token] = out
	p.search.Unlock()
	return token
}

func (p *adcPeer) Search(ctx context.Context, req SearchRequest, out Search) error {
	token := ""
	if as, ok := out.(*adcSearch); ok {
		token = as.token
	} else {
		token = p.searchToken(out)
	}
	msg := adc.SearchRequest{
		Token: token,
	}
	if r, ok := req.(TTHSearch); ok {
		msg.TTH = (*TTH)(&r)
	} else {
		var name NameSearch
		switch r := req.(type) {
		case NameSearch:
			name = r
		case DirSearch:
			name = r.NameSearch
			msg.Type = adc.FileTypeDir
		case FileSearch:
			name = r.NameSearch
			msg.Type = adc.FileTypeFile
			if r.MinSize != 0 {
				msg.Ge = int64(r.MinSize)
			}
			if r.MaxSize != 0 {
				msg.Le = int64(r.MaxSize)
			}
			if r.MinSize == r.MaxSize {
				msg.Le, msg.Ge = 0, 0
				msg.Eq = int64(r.MaxSize)
			}
			msg.Ext = r.Ext
			msg.NoExt = r.NoExt
			switch r.FileType {
			case FileTypeAudio:
				msg.Group = adc.ExtAudio
			case FileTypeCompressed:
				msg.Group = adc.ExtArch
			case FileTypeDocuments:
				msg.Group = adc.ExtDoc
			case FileTypeExecutable:
				msg.Group = adc.ExtExe
			case FileTypePicture:
				msg.Group = adc.ExtImage
			case FileTypeVideo:
				msg.Group = adc.ExtVideo
			}
		default:
			return nil // ignore
		}
		msg.And = name.And
		msg.Not = name.Not
	}
	err := p.conn.WriteBroadcast(out.Peer().SID(), msg)
	if err == nil {
		err = p.conn.Flush()
	}
	return err
}

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

	adcp "github.com/direct-connect/go-dc/adc"
	"github.com/direct-connect/go-dc/adc/types"
	"github.com/direct-connect/go-dc/tiger"
	dctypes "github.com/direct-connect/go-dc/types"
	"github.com/direct-connect/go-dcpp/adc"
	"github.com/direct-connect/go-dcpp/internal/safe"
)

const searchTimeout = time.Minute

type SID = adc.SID

type CID = adc.CID

func sidFromInt(v uint32) SID {
	return types.SIDFromInt(v)
}

type adcPeers struct {
	loggingCID map[adc.CID]struct{}
	byCID      map[adc.CID]*adcPeer
}

func (h *Hub) initADC() {
	h.peers.loggingCID = make(map[adc.CID]struct{})
	h.peers.byCID = make(map[adc.CID]*adcPeer)
}

func (h *Hub) ServeADC(conn net.Conn, cinfo *ConnInfo) error {
	cntConnADC.Add(1)
	cntConnADCOpen.Add(1)
	defer cntConnADCOpen.Add(-1)
	if cinfo.TLSVers != 0 {
		cntConnADCS.Add(1)
	}
	if cinfo.ALPN != "" {
		cntConnAlpnADC.Add(1)
	}

	log.Printf("%s: using ADC", conn.RemoteAddr())
	c, err := adc.NewConn(conn)
	if err != nil {
		return err
	}
	defer c.Close()
	c.SetWriteTimeout(writeTimeout)
	c.OnLineR(func(line []byte) (bool, error) {
		sizeADCLinesR.Observe(float64(len(line)))
		if h.sampler.enabled() {
			h.sampler.sample(line)
		}
		return true, nil
	})
	c.OnLineW(func(line []byte) (bool, error) {
		sizeADCLinesW.Observe(float64(len(line)))
		return true, nil
	})

	peer, err := h.adcHandshake(c, cinfo)
	if err != nil {
		return err
	}
	defer peer.Close()
	return h.adcServePeer(peer)
}

func (h *Hub) adcHandshake(c *adc.Conn, cinfo *ConnInfo) (*adcPeer, error) {
	defer measure(durADCHandshake)()

	peer, err := h.adcStageProtocol(c, cinfo)
	if err != nil {
		return nil, err
	}
	// connection is not yet valid and we haven't added the client to the hub yet
	if err = h.adcStageIdentity(peer); err != nil {
		return nil, err
	}
	// TODO: identify pingers
	// peer registered, now we can start serving things
	if err = peer.HubChatMsg(topicMsg(h.getTopic())); err != nil {
		_ = peer.Close()
		return nil, err
	}
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
	if !h.callOnJoined(peer) {
		return nil // TODO: eny errors?
	}
	// looks like we are disabling the timeout, but we are not
	// the timeout will be set manually by the writer goroutine
	peer.c.SetWriteTimeout(-1)
	go peer.writer(writeTimeout)
	for {
		p, err := peer.c.ReadPacketRaw(time.Time{})
		if err == io.EOF {
			return nil
		} else if err != nil {
			if !peer.Online() {
				return nil
			}
			return err
		}
		if err = h.adcHandlePacket(peer, p); err != nil {
			return err
		}
	}
}

func (h *Hub) adcHandlePacket(peer *adcPeer, p adcp.Packet) error {
	skind := string(p.Kind())
	cntADCPackets.WithLabelValues(skind).Add(1)
	defer measure(durADCHandlePacket.WithLabelValues(skind))()

	cmd := p.Message().Cmd().String()
	cntADCCommands.WithLabelValues(cmd).Add(1)
	defer measure(durADCHandleCommand.WithLabelValues(cmd))()

	switch p := p.(type) {
	case *adcp.BroadcastPacket:
		if peer.sid != p.ID {
			return errors.New("malformed broadcast")
		}
		h.adcBroadcast(p, peer)
		return nil
	case *adcp.FeaturePacket:
		if peer.sid != p.ID {
			return errors.New("malformed features broadcast")
		}
		// TODO: we ignore feature selectors for now
		h.adcBroadcast(&adcp.BroadcastPacket{Msg: p.Msg, ID: p.ID}, peer)
		return nil
	case *adcp.EchoPacket:
		if peer.sid != p.ID {
			return errors.New("malformed echo packet")
		}
		if err := peer.SendADC(p); err != nil {
			return err
		}
		h.adcDirect((*adcp.DirectPacket)(p), peer)
		return nil
	case *adcp.DirectPacket:
		if peer.sid != p.ID {
			return errors.New("malformed direct packet")
		}
		h.adcDirect(p, peer)
		return nil
	case *adcp.HubPacket:
		h.adcHub(p, peer)
		return nil
	case *adcp.ClientPacket, *adcp.UDPPacket:
		return errors.New("invalid packet kind")
	default:
		raw, _ := p.Message().(*adcp.RawMessage)
		log.Printf("%s: adc: %s%s %s", peer.RemoteAddr(), string(p.Kind()), p.Message().Cmd(), string(raw.Data))
		return nil
	}
}

func (h *Hub) adcStageProtocol(c *adc.Conn, cinfo *ConnInfo) (*adcPeer, error) {
	deadline := time.Now().Add(time.Second * 5)
	// Expect features from the client
	p, err := c.ReadPacketRaw(deadline)
	if err != nil {
		return nil, err
	}
	hp, ok := p.(*adcp.HubPacket)
	if !ok {
		return nil, fmt.Errorf("expected hub messagge, got: %#v", p)
	}
	var sup adcp.Supported
	if err := hp.DecodeMessageTo(&sup); err != nil {
		return nil, err
	}
	for ext, on := range sup.Features {
		if !on {
			continue
		}
		cntADCExtensions.WithLabelValues(ext.String()).Add(1)
	}
	hubFeatures := adcp.ModFeatures{
		// should always be set for ADC
		adcp.FeaBASE: true,
		adcp.FeaBAS0: true,
		adcp.FeaTIGR: true,
		// extensions
		adcp.FeaPING: true,
		adcp.FeaUCMD: true,
		adcp.FeaUCM0: true,
		adcp.FeaZLIF: true,
	}

	mutual := hubFeatures.Intersect(sup.Features)
	if !mutual.IsSet(adcp.FeaBASE) && !mutual.IsSet(adcp.FeaBAS0) {
		return nil, fmt.Errorf("client does not support BASE")
	} else if !mutual.IsSet(adcp.FeaTIGR) {
		return nil, fmt.Errorf("client does not support TIGR")
	}

	if lvl := h.zlibLevel(); lvl != 0 && mutual.IsSet(adcp.FeaZLIF) {
		err = c.WriteInfoMsg(adcp.ZOn{})
		if err != nil {
			return nil, err
		}
		err = c.ZOn(lvl)
		if err != nil {
			return nil, err
		}
	}

	// send features supported by the hub
	err = c.WriteInfoMsg(adcp.Supported{
		Features: hubFeatures,
	})
	if err != nil {
		return nil, err
	}

	peer := newADC(h, cinfo, c, mutual)

	err = c.WriteInfoMsg(adcp.SIDAssign{
		SID: peer.SID(),
	})
	if err != nil {
		return nil, err
	}
	err = c.Flush()
	if err != nil {
		return nil, err
	}
	return peer, nil
}

func (h *Hub) adcStageIdentity(peer *adcPeer) error {
	deadline := time.Now().Add(time.Second * 5)
	// client should send INF with ID and PID set
	p, err := peer.c.ReadPacketRaw(deadline)
	if err != nil {
		return err
	}
	b, ok := p.(*adcp.BroadcastPacket)
	if !ok {
		return fmt.Errorf("expected user info broadcast, got %#v", p)
	}
	var u adcp.UserInfo
	if err := b.DecodeMessageTo(&u); err != nil {
		return err
	}
	u.Normalize()
	cntClients.WithLabelValues(u.Application, u.Version).Add(1)
	for _, ext := range u.Features {
		cntADCExtensions.WithLabelValues(ext.String()).Add(1)
	}
	if u.Id != u.Pid.Hash() {
		err = errors.New("invalid pid supplied")
		_ = peer.sendErrorNow(adcp.Fatal, 27, err)
		return err
	}
	u.Pid = nil

	err = h.validateUserName(u.Name)
	if err != nil {
		_ = peer.sendErrorNow(adcp.Fatal, 21, err)
		return err
	}

	// do not lock for writes first
	sameCID := false
	sameName := !h.nameAvailable(u.Name, func() {
		_, sameCID1 := h.peers.loggingCID[u.Id]
		_, sameCID2 := h.peers.byCID[u.Id]
		sameCID = sameCID1 || sameCID2
	})

	if sameName {
		err = errNickTaken
		_ = peer.sendErrorNow(adcp.Fatal, 22, err)
		return err
	}
	if sameCID {
		err = errors.New("CID taken")
		_ = peer.sendErrorNow(adcp.Fatal, 24, err)
		return err
	}

	// ok, now lock for writes and try to bind nick and CID
	// still, no one will see the user yet
	unbind, ok := h.reserveName(u.Name, func() bool {
		_, sameCID1 := h.peers.loggingCID[u.Id]
		_, sameCID2 := h.peers.byCID[u.Id]
		if sameCID1 || sameCID2 {
			sameCID = true
			return false
		}
		h.peers.loggingCID[u.Id] = struct{}{}
		return true
	}, func() {
		delete(h.peers.loggingCID, u.Id)
	})
	if !ok {
		if sameCID {
			err = errors.New("CID taken")
			_ = peer.sendErrorNow(adcp.Fatal, 24, err)
			return err
		}
		err = errNickTaken
		_ = peer.sendErrorNow(adcp.Fatal, 22, err)
		return err
	}

	if u.Ip4 == "0.0.0.0" {
		if t, ok := peer.RemoteAddr().(*net.TCPAddr); ok {
			if ip4 := t.IP.To4(); ip4 != nil {
				u.Ip4 = ip4.String()
			}
		}
	}
	if u.Ip6 == "::" {
		if t, ok := peer.RemoteAddr().(*net.TCPAddr); ok {
			if ip6 := t.IP.To16(); ip6 != nil {
				u.Ip6 = ip6.String()
			}
		}
	}
	u.Normalize()
	peer.setName(u.Name)
	peer.info.cid = u.Id
	peer.info.user = u

	st := h.Stats()
	if err := h.adcStageVerify(peer); err != nil {
		unbind()
		return err
	}
	deadline = time.Now().Add(time.Second * 5)

	// send hub info
	err = peer.c.WriteInfoMsg(adcp.HubInfo{
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
	err = peer.c.WriteInfoMsg(adcp.Status{
		Sev:  adcp.Success,
		Code: 0,
		Msg:  h.poweredBy(),
	})

	// send user list (except his own info)
	err = peer.peersJoin(&PeersJoinEvent{Peers: h.Peers()}, true)
	if err != nil {
		unbind()
		return err
	}

	// write his info and flush
	err = peer.peersJoin(&PeersJoinEvent{Peers: []Peer{peer}}, true)
	if err != nil {
		unbind()
		return err
	}

	if peer.fea.IsSet(adcp.FeaUCMD) || peer.fea.IsSet(adcp.FeaUCM0) {
		err = h.adcSendUserCommand(peer)
		if err != nil {
			unbind()
			return err
		}
	}

	var list []Peer
	// finally accept the user on the hub
	h.acceptPeer(peer, func() {
		delete(h.peers.loggingCID, u.Id)
		h.peers.byCID[u.Id] = peer
		// make a snapshot of peers to send info to
		list = h.listPeers()
	}, nil)
	// notify other users about the new one
	h.broadcastUserJoin(peer, list)
	return peer.c.Flush()
}

func (h *Hub) adcStageVerify(peer *adcPeer) error {
	user, rec, err := h.getUser(peer.Name())
	if err != nil {
		return err
	} else if user == nil || rec == nil {
		return nil
	}
	if c := peer.ConnInfo(); c != nil && !c.Secure {
		return errConnInsecure
	}
	// give the user a minute to enter a password
	deadline := time.Now().Add(time.Minute)
	//some bytes for check password
	var salt [24]byte
	rand.Read(salt[:])
	err = peer.c.WriteInfoMsg(adcp.GetPassword{
		Salt: salt[:],
	})
	if err != nil {
		return err
	}
	err = peer.c.Flush()
	if err != nil {
		return err
	}

	p, err := peer.c.ReadPacketRaw(deadline)
	if err != nil {
		return err
	}
	hp, ok := p.(*adcp.HubPacket)
	if !ok {
		return fmt.Errorf("expected hub messagge, got: %#v", p)
	}
	var pass adcp.Password
	if err := hp.DecodeMessageTo(&pass); err != nil {
		return err
	}
	ok, err = h.adcCheckUserPass(rec, salt[:], pass.Hash)
	if err != nil {
		return err
	} else if !ok {
		err = errors.New("wrong password")
		_ = peer.sendErrorNow(adcp.Fatal, 23, err)
		return err
	}
	peer.setUser(user)
	return nil
}

func (h *Hub) adcCheckUserPass(rec *UserRecord, salt []byte, hash tiger.Hash) (bool, error) {
	if h.db == nil {
		return false, nil
	}
	check := make([]byte, len(rec.Pass)+len(salt))
	i := copy(check, rec.Pass)
	copy(check[i:], salt)
	exp := tiger.HashBytes(check)
	return exp == hash, nil
}

func (h *Hub) adcHub(p *adcp.HubPacket, from Peer) {
	// TODO: disallow INF, STA and some others
	err := p.DecodeMessage()
	if err != nil {
		log.Printf("cannot parse ADC message: %v", err)
		return
	}
	switch msg := p.Msg.(type) {
	case adcp.ChatMessage:
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
	for _, c := range h.ListCommands(peer.User()) {
		cat := adcp.CategoryHub
		cmd := "HMSG !" + c.Name
		if c.opt.OnUser {
			cmd += `\s%[userSID]`
			cat = adcp.CategoryUser
		}
		cmd += "\n"
		err := peer.c.WriteInfoMsg(adcp.UserCommand{
			Path:     c.Menu,
			Command:  cmd,
			Category: cat,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (h *Hub) adcBroadcast(p *adcp.BroadcastPacket, from *adcPeer) {
	var err error
	if p.Msg.Cmd() == (adcp.UserInfo{}).Cmd() {
		var m adcp.UserInfoMod
		err = p.DecodeMessageTo(&m)
		p.Msg = &m
	} else {
		err = p.DecodeMessage()
	}
	if err != nil {
		log.Printf("cannot parse ADC message: %v", err)
		return
	}
	// TODO: read INF, update peer info
	// TODO: update nick, make sure there is no duplicates
	// TODO: disallow STA and some others
	switch msg := p.Msg.(type) {
	case adcp.ChatMessage:
		if h.isCommand(from, msg.Text) {
			return
		}
		h.globalChat.SendChat(from, Message{
			Text: msg.Text,
			Me:   msg.Me,
		})
	case adcp.SearchRequest:
		h.adcHandleSearch(from, &msg, nil)
	default:
		// TODO: decode other packets
		for _, peer := range h.Peers() {
			if p2, ok := peer.(*adcPeer); ok {
				_ = p2.SendADC(p)
			}
		}
	}
}

func (h *Hub) adcDirect(p *adcp.DirectPacket, from *adcPeer) {
	// TODO: disallow INF, STA and some others
	peer := h.peerBySID(p.To)
	if peer == nil {
		r := h.roomBySID(p.To)
		if r == nil {
			return
		}
		err := p.DecodeMessage()
		if err != nil {
			log.Printf("cannot parse ADC message: %v", err)
			return
		}
		switch msg := p.Msg.(type) {
		case adcp.ChatMessage:
			r.SendChat(from, Message{
				Text: msg.Text,
				Me:   msg.Me,
			})
		}
		return
	}
	err := p.DecodeMessage()
	if err != nil {
		log.Printf("cannot parse ADC message: %v", err)
		return
	}
	switch msg := p.Msg.(type) {
	case adcp.ChatMessage:
		h.privateChat(from, peer, Message{
			Name: from.Name(),
			Text: string(msg.Text),
		})
	case adcp.ConnectRequest:
		info := from.Info()
		ip := info.Ip4
		if info.Ip6 != "" {
			ip = info.Ip6
		}
		if ip == "" {
			return
		}
		secure := strings.HasPrefix(msg.Proto, "ADCS")
		h.connectReq(from, peer, ip+":"+strconv.Itoa(msg.Port), msg.Token, secure)
	case adcp.RevConnectRequest:
		secure := strings.HasPrefix(msg.Proto, "ADCS")
		h.revConnectReq(from, peer, msg.Token, secure)
	case adcp.SearchRequest:
		h.adcHandleSearch(from, &msg, []Peer{peer})
	case adcp.SearchResult:
		h.adcHandleResult(from, peer, &msg)
	default:
		// TODO: decode other packets
		if p2, ok := peer.(*adcPeer); ok {
			_ = p2.SendADC(p)
		}
	}
}

func (h *Hub) adcHandleSearch(peer *adcPeer, req *adcp.SearchRequest, peers []Peer) {
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
	if req.Type == adcp.FileTypeFile ||
		req.Eq != 0 || req.Le != 0 || req.Ge != 0 ||
		len(req.Ext) != 0 || len(req.NoExt) != 0 ||
		req.Group != adcp.ExtNone {
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
		case adcp.ExtAudio:
			freq.FileType = FileTypeAudio
		case adcp.ExtArch:
			freq.FileType = FileTypeCompressed
		case adcp.ExtDoc:
			freq.FileType = FileTypeDocuments
		case adcp.ExtExe:
			freq.FileType = FileTypeExecutable
		case adcp.ExtImage:
			freq.FileType = FileTypePicture
		case adcp.ExtVideo:
			freq.FileType = FileTypeVideo
		}
		sr = freq
	}
	h.Search(sr, s, peers)
}

func (h *Hub) adcHandleResult(peer *adcPeer, to Peer, res *adcp.SearchResult) {
	if to, ok := to.(*adcPeer); ok {
		_ = to.SendADCDirect(peer.SID(), *res)
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
	if err := s.s.SendResult(sr); err != nil {
		_ = s.s.Close()
		peer.search.Lock()
		delete(peer.search.tokens, res.Token)
		peer.search.Unlock()
	} else {
		s.last.SetNow()
	}
}

var _ Peer = (*adcPeer)(nil)

func newADC(h *Hub, cinfo *ConnInfo, c *adc.Conn, fea adcp.ModFeatures) *adcPeer {
	if cinfo == nil {
		cinfo = &ConnInfo{Local: c.LocalAddr(), Remote: c.RemoteAddr()}
	}
	peer := &adcPeer{
		c:   c,
		fea: fea,
	}
	cinfo.Proto = "ADC"
	h.newBasePeer(&peer.BasePeer, cinfo)
	peer.write.wake = make(chan struct{}, 1)
	return peer
}

type adcPeer struct {
	BasePeer

	c   *adc.Conn
	fea adcp.ModFeatures

	write struct {
		wake chan struct{}
		sync.Mutex
		buf []adcp.Packet
	}
	info struct {
		cid adc.CID

		sync.RWMutex
		user adcp.UserInfo
	}

	search struct {
		sync.RWMutex
		tokens map[string]*adcSearchToken
	}
}

func (p *adcPeer) Searchable() bool {
	p.info.RLock()
	share := p.info.user.ShareSize
	p.info.RUnlock()
	return share > 0
}

type adcSearchToken struct {
	last safe.Time
	s    Search
}

func adcUserType(u *adcp.UserInfo, c *User, info *UserInfo) {
	u.Type = adcp.UserTypeNone
	if info != nil {
		switch info.Kind {
		case UserBot:
			u.Type = adcp.UserTypeBot
		case UserHub:
			u.Type = adcp.UserTypeHub
		}
	}
	if c == nil {
		return
	}
	if c.Has(PermOwner) {
		u.Type = adcp.UserTypeHubOwner
	} else if c.Has(FlagOpIcon) {
		u.Type = adcp.UserTypeOperator
	} else if c.Has(FlagRegIcon) {
		u.Type = adcp.UserTypeRegistered
	}
}

func (p *adcPeer) Info() adcp.UserInfo {
	p.info.RLock()
	u := p.info.user
	p.info.RUnlock()
	adcUserType(&u, p.User(), nil)
	return u
}

func (p *adcPeer) UserInfo() UserInfo {
	u := p.Info()
	return UserInfo{
		Name:  u.Name,
		Share: uint64(u.ShareSize),
		Email: u.Email,
		Desc:  u.Desc,
		App: dctypes.Software{
			Name:    u.Application,
			Version: u.Version,
		},
		HubsNormal:     u.HubsNormal,
		HubsRegistered: u.HubsRegistered,
		HubsOperator:   u.HubsOperator,
		Slots:          u.Slots,
		IPv4:           u.Features.Has(adcp.FeaTCP4),
		IPv6:           u.Features.Has(adcp.FeaTCP6),
		TLS:            u.Features.Has(adcp.FeaADC0),
	}
}

func (p *adcPeer) writer(timeout time.Duration) {
	defer p.Close()
	ticker := time.NewTicker(time.Minute / 2)
	defer ticker.Stop()

	var buf2 []adcp.Packet
	for {
		var err error
		select {
		case <-p.close.done:
			return
		case <-ticker.C:
			// keep alive
			_ = p.c.SetWriteDeadline(time.Now().Add(timeout))
			err = p.c.WriteKeepAlive()
		case <-p.write.wake:
			p.write.Lock()
			buf := p.write.buf
			p.write.buf = buf2
			p.write.Unlock()
			if len(buf) == 0 {
				buf2 = buf[:0]
				continue
			}
			_ = p.c.SetWriteDeadline(time.Now().Add(timeout))
			for i, m := range buf {
				err = p.c.WritePacket(m)
				if err != nil {
					break
				}
				buf[i] = nil
			}
			buf2 = buf[:0]
		}
		if err == nil {
			err = p.c.Flush()
		}
		_ = p.c.SetWriteDeadline(time.Time{})
		if err != nil {
			if p.Online() {
				log.Printf("%s: write: %v", p.c.RemoteAddr(), err)
			}
			return
		}
	}
}

func (p *adcPeer) SendADC(m ...adcp.Packet) error {
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

func (p *adcPeer) SendADCInfo(m adcp.Message) error {
	return p.SendADC(&adcp.InfoPacket{
		Msg: m,
	})
}

func (p *adcPeer) SendADCDirect(from SID, m adcp.Message) error {
	return p.SendADC(&adcp.DirectPacket{
		ID: from, To: p.SID(),
		Msg: m,
	})
}

func (p *adcPeer) SendADCBroadcast(from SID, m adcp.Message) error {
	return p.SendADC(&adcp.BroadcastPacket{
		ID:  from,
		Msg: m,
	})
}

func (p *adcPeer) sendInfoNow(m adcp.Message) error {
	err := p.c.WriteInfoMsg(m)
	if err != nil {
		return err
	}
	return p.c.Flush()
}

func (p *adcPeer) sendErrorNow(sev adcp.Severity, code int, err error) error {
	return p.sendInfoNow(adcp.Status{
		Sev: sev, Code: code, Msg: err.Error(),
	})
}

func (p *adcPeer) Close() error {
	return p.closeWith(p,
		p.c.Close,
		func() error {
			p.hub.leaveCID(p, p.sid, p.info.cid)
			return nil
		},
	)
}

func (p *adcPeer) PeersJoin(e *PeersJoinEvent) error {
	return p.peersJoin(e, false)
}

func (p *adcPeer) PeersUpdate(e *PeersUpdateEvent) error {
	// TODO: diff infos
	return p.peersUpdate(e)
}

func (u UserInfo) toADC(cid CID, user *User) adcp.UserInfo {
	out := adcp.UserInfo{
		Name:           u.Name,
		Id:             cid,
		Application:    u.App.Name,
		Version:        u.App.Version,
		HubsNormal:     u.HubsNormal,
		HubsRegistered: u.HubsRegistered,
		HubsOperator:   u.HubsOperator,
		Slots:          u.Slots,
		ShareSize:      int64(u.Share),
		Email:          u.Email,
		Desc:           u.Desc,
	}
	adcUserType(&out, user, &u)
	if u.TLS {
		out.Features = append(out.Features, adcp.FeaADC0)
	}
	if u.IPv4 {
		out.Features = append(out.Features, adcp.FeaTCP4)
	}
	if u.IPv6 {
		out.Features = append(out.Features, adcp.FeaTCP6)
	}
	return out
}

func (p *adcPeer) fixUserInfo(u *adcp.UserInfo) {
	if u.Application != "" && p.Info().Application == "" {
		// doesn't support AP field
		u.Application, u.Version = "", u.Application+" "+u.Version
	}
}

func (p *adcPeer) peersJoin(e *PeersJoinEvent, initial bool) error {
	if !p.Online() {
		return errConnectionClosed
	}
	for _, peer := range e.Peers {
		var u adcp.UserInfo
		if p2, ok := peer.(*adcPeer); ok {
			u = p2.Info()
		} else {
			// TODO: same address from multiple clients behind NAT, so we addend the name
			addr, _, _ := net.SplitHostPort(peer.RemoteAddr().String())
			// TODO: once we support name changes, we should make the user
			//       virtually leave and rejoin with a new CID
			cid := adc.CID(tiger.HashBytes([]byte(peer.Name() + "\x00" + addr)))
			u = peer.UserInfo().toADC(cid, peer.User())
			if t, ok := peer.RemoteAddr().(*net.TCPAddr); ok {
				if ip4 := t.IP.To4(); ip4 != nil {
					u.Ip4 = ip4.String()
				} else {
					u.Ip6 = t.IP.String()
				}
			}
		}
		p.fixUserInfo(&u)
		if !p.Online() {
			return errConnectionClosed
		}
		var err error
		if initial {
			err = p.c.WriteBroadcast(peer.SID(), &u)
		} else {
			err = p.SendADCBroadcast(peer.SID(), &u)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (p *adcPeer) peersUpdate(e *PeersUpdateEvent) error {
	if !p.Online() {
		return errConnectionClosed
	}
	for _, peer := range e.Peers {
		var u adcp.UserInfo
		if p2, ok := peer.(*adcPeer); ok {
			u = p2.Info()
		} else {
			u = peer.UserInfo().toADC(CID{}, peer.User())
		}
		p.fixUserInfo(&u)
		if !p.Online() {
			return errConnectionClosed
		}
		err := p.SendADCBroadcast(peer.SID(), &u)
		if err != nil {
			return err
		}
	}
	return nil
}

func (p *adcPeer) PeersLeave(e *PeersLeaveEvent) error {
	if !p.Online() {
		return errConnectionClosed
	}
	for _, peer := range e.Peers {
		if !p.Online() {
			return errConnectionClosed
		}
		if err := p.SendADCInfo(&adcp.Disconnect{
			ID: peer.SID(),
		}); err != nil {
			return err
		}
	}
	return nil
}

func (p *adcPeer) JoinRoom(room *Room) error {
	if !p.Online() {
		return errConnectionClosed
	}
	if room.Name() == "" {
		return nil
	}
	rsid := room.SID()
	rname := room.Name()
	h := tiger.HashBytes([]byte(rname)) // TODO: include hub name?
	soft := p.hub.getSoft()
	err := p.SendADCBroadcast(rsid, adcp.UserInfo{
		Id:          types.CID(h),
		Name:        rname,
		HubsNormal:  room.Users(), // TODO: update
		Application: soft.Name,
		Version:     soft.Version,
		Type:        adcp.UserTypeOperator,
		Slots:       1,
	})
	if err != nil {
		return err
	}
	err = p.SendADCDirect(rsid, adcp.ChatMessage{
		Text: "joined", PM: &rsid, Me: true,
	})
	if err != nil {
		return err
	}
	return nil
}

func (p *adcPeer) LeaveRoom(room *Room) error {
	if !p.Online() {
		return errConnectionClosed
	}
	if room.Name() == "" {
		return nil
	}
	rsid := room.SID()
	err := p.SendADCDirect(rsid, adcp.ChatMessage{
		Text: "parted", PM: &rsid, Me: true,
		TS: time.Now().Unix(),
	})
	if err != nil {
		return err
	}
	err = p.SendADCBroadcast(rsid, adcp.Disconnect{
		ID: rsid,
	})
	if err != nil {
		return err
	}
	return nil
}

func (p *adcPeer) ChatMsg(room *Room, from Peer, msg Message) error {
	if !p.Online() {
		return errConnectionClosed
	}
	if room == nil || room.Name() == "" {
		return p.SendADCBroadcast(from.SID(), &adcp.ChatMessage{
			Text: msg.Text, Me: msg.Me,
			TS: msg.Time.Unix(),
		})

	}
	if p == from {
		return nil // no echo
	}
	rsid := room.SID()
	fsid := from.SID()
	return p.SendADCDirect(fsid, adcp.ChatMessage{
		Text: msg.Text, PM: &rsid, Me: msg.Me,
		TS: msg.Time.Unix(),
	})
}

func (p *adcPeer) PrivateMsg(from Peer, msg Message) error {
	if !p.Online() {
		return errConnectionClosed
	}
	src := from.SID()
	return p.SendADCDirect(src, &adcp.ChatMessage{
		Text: msg.Text, PM: &src, Me: msg.Me,
		TS: msg.Time.Unix(),
	})
}

func (p *adcPeer) HubChatMsg(m Message) error {
	if !p.Online() {
		return errConnectionClosed
	}
	if m.Time.IsZero() {
		m.Time = time.Now()
	}
	return p.SendADCInfo(&adcp.ChatMessage{
		Text: m.Text, Me: m.Me,
		TS: m.Time.Unix(),
	})
}

func (p *adcPeer) ConnectTo(peer Peer, addr string, token string, secure bool) error {
	if !p.Online() {
		return errConnectionClosed
	}
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
	err = p.SendADCBroadcast(peer.SID(), &adcp.UserInfoMod{
		{Tag: field, Value: host},
	})
	if err != nil {
		return err
	}

	// we need to pretend that peer speaks the same protocol as we do
	proto := adcp.ProtoADC
	if secure {
		proto = adcp.ProtoADCS
	}
	return p.SendADCDirect(peer.SID(), &adcp.ConnectRequest{
		Proto: proto,
		Port:  port,
		Token: token,
	})
}

func (p *adcPeer) RevConnectTo(peer Peer, token string, secure bool) error {
	if !p.Online() {
		return errConnectionClosed
	}
	// we need to pretend that peer speaks the same protocol as we do
	proto := adcp.ProtoADC
	if secure {
		proto = adcp.ProtoADCS
	}
	return p.SendADCDirect(peer.SID(), &adcp.RevConnectRequest{
		Proto: proto,
		Token: token,
	})
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
	if !s.p.Online() {
		return errConnectionClosed
	}
	sr := adcp.SearchResult{
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
	return s.p.SendADCDirect(r.From().SID(), sr)
}

func (s *adcSearch) Close() error {
	return nil // TODO: block new results
}

// gcTokens removes unused search tokens.
// Should be called under the search write lock.
func (p *adcPeer) gcTokens() {
	now := time.Now()
	for token, s := range p.search.tokens {
		if now.Sub(s.last.Get()) > searchTimeout {
			delete(p.search.tokens, token)
			_ = s.s.Close()
		}
	}
}

func (p *adcPeer) searchToken(out Search) string {
	token := strconv.FormatUint(rand.Uint64(), 16)
	p.search.Lock()
	defer p.search.Unlock()
	if p.search.tokens == nil {
		p.search.tokens = make(map[string]*adcSearchToken)
	} else {
		p.gcTokens()
	}
	s := &adcSearchToken{s: out}
	s.last.SetNow()
	p.search.tokens[token] = s
	return token
}

func (p *adcPeer) Search(ctx context.Context, req SearchRequest, out Search) error {
	if !p.Online() {
		return errConnectionClosed
	}
	token := ""
	if as, ok := out.(*adcSearch); ok {
		token = as.token
	} else {
		token = p.searchToken(out)
	}
	msg := adcp.SearchRequest{
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
			msg.Type = adcp.FileTypeDir
		case FileSearch:
			name = r.NameSearch
			msg.Type = adcp.FileTypeFile
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
				msg.Group = adcp.ExtAudio
			case FileTypeCompressed:
				msg.Group = adcp.ExtArch
			case FileTypeDocuments:
				msg.Group = adcp.ExtDoc
			case FileTypeExecutable:
				msg.Group = adcp.ExtExe
			case FileTypePicture:
				msg.Group = adcp.ExtImage
			case FileTypeVideo:
				msg.Group = adcp.ExtVideo
			}
		default:
			return nil // ignore
		}
		msg.And = name.And
		msg.Not = name.Not
	}
	return p.SendADCBroadcast(out.Peer().SID(), msg)
}

func (p *adcPeer) Redirect(addr string) error {
	if !p.Online() {
		return errConnectionClosed
	}
	return p.SendADCInfo(&adcp.Disconnect{
		ID:       p.SID(),
		Redirect: addr,
	})
}

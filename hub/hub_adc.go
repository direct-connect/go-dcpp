package hub

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/direct-connect/go-dcpp/adc"
	"github.com/direct-connect/go-dcpp/tiger"
)

func (h *Hub) initADC() {
	h.peers.loggingCID = make(map[adc.CID]struct{})
	h.peers.byCID = make(map[adc.CID]*adcPeer)
}

func (h *Hub) ServeADC(conn net.Conn) error {
	log.Printf("%s: using ADC", conn.RemoteAddr())
	c, err := adc.NewConn(conn)
	if err != nil {
		return err
	}
	defer c.Close()

	peer, err := h.adcStageProtocol(c)
	if err != nil {
		return err
	}
	// connection is not yet valid and we haven't added the client to the hub yet
	if err := h.adcStageIdentity(peer); err != nil {
		return err
	}
	// peer registered, now we can start serving things
	defer peer.Close()

	if err = h.sendMOTD(peer); err != nil {
		return err
	}

	return h.adcServePeer(peer)
}

func (h *Hub) adcServePeer(peer *adcPeer) error {
	peer.conn.KeepAlive(time.Minute / 2)
	for {
		p, err := peer.conn.ReadPacket(time.Time{})
		if err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}
		switch p := p.(type) {
		case *adc.BroadcastPacket:
			if peer.sid != p.ID {
				return fmt.Errorf("malformed broadcast")
			}
			// TODO: read INF, update peer info
			// TODO: update nick, make sure there is no duplicates
			// TODO: disallow STA and some others
			go h.adcBroadcast(p, peer, h.Peers())
		case *adc.EchoPacket:
			if peer.sid != p.ID {
				return fmt.Errorf("malformed echo packet")
			}
			if err := peer.conn.WritePacket(p); err != nil {
				return err
			}
			if err = peer.conn.Flush(); err != nil {
				return err
			}
			// TODO: disallow INF, STA and some others
			go h.adcDirect((*adc.DirectPacket)(p), peer)
		case *adc.DirectPacket:
			if peer.sid != p.ID {
				return fmt.Errorf("malformed direct packet")
			}
			// TODO: disallow INF, STA and some others
			go h.adcDirect(p, peer)
		default:
			data, _ := p.MarshalPacket()
			log.Printf("%s: adc: %s", peer.RemoteAddr(), string(data))
		}
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
	hubFeatures := adc.ModFeatures{
		// should always be set for ADC
		adc.FeaBASE: true,
		adc.FeaBAS0: true,
		adc.FeaTIGR: true,
		// extensions
		adc.FeaPING: true,
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
			hub:  h,
			addr: c.RemoteAddr(),
			sid:  sid,
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
	if u.Id != u.Pid.Hash() {
		err = errors.New("invalid pid supplied")
		_ = peer.sendError(adc.Fatal, 27, err)
		return err
	}
	u.Pid = nil
	if u.Name == "" {
		err = errors.New("invalid nick")
		_ = peer.sendError(adc.Fatal, 21, err)
		return err
	}

	// do not lock for writes first
	h.peers.RLock()
	_, sameName1 := h.peers.logging[u.Name]
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
	_, sameName1 = h.peers.logging[u.Name]
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
	h.peers.logging[u.Name] = struct{}{}
	h.peers.loggingCID[u.Id] = struct{}{}
	h.peers.Unlock()

	unbind := func() {
		h.peers.Lock()
		delete(h.peers.logging, u.Name)
		delete(h.peers.loggingCID, u.Id)
		h.peers.Unlock()
	}

	if u.Ip4 == "0.0.0.0" {
		ip, _, _ := net.SplitHostPort(peer.addr.String())
		if ip != "" {
			u.Ip4 = ip
		}
	}
	peer.user = u

	// send hub info
	err = peer.conn.WriteInfoMsg(adc.HubInfo{
		Name:    h.info.Name,
		Version: h.info.Soft.Name + " " + h.info.Soft.Vers,
		Desc:    h.info.Desc,
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

	// finally accept the user on the hub
	h.peers.Lock()
	// cleanup temporary bindings
	delete(h.peers.logging, peer.user.Name)
	delete(h.peers.loggingCID, u.Id)

	// make a snapshot of peers to send info to
	list := h.listPeers()

	// add user to the hub
	h.peers.bySID[peer.sid] = peer
	h.peers.byCID[u.Id] = peer
	h.peers.byName[u.Name] = peer
	h.peers.Unlock()

	// notify other users about the new one
	// TODO: this will block the client
	h.broadcastUserJoin(peer, list)
	return nil
}

func (h *Hub) adcBroadcast(p *adc.BroadcastPacket, from Peer, peers []Peer) {
	if peers == nil {
		peers = h.Peers()
	}
	var nmdc []Peer
	for _, peer := range peers {
		if p2, ok := peer.(*adcPeer); ok {
			_ = p2.conn.WritePacket(p)
			_ = p2.conn.Flush()
		} else {
			nmdc = append(nmdc, peer)
		}
	}
	if len(nmdc) == 0 {
		return
	}
	msg, err := p.Decode()
	if err != nil {
		log.Printf("cannot parse ADC message: %v", err)
		return
	}
	switch msg := msg.(type) {
	case adc.ChatMessage:
		h.broadcastChat(from, string(msg.Text), nmdc)
	default:
		// TODO: decode other packets
	}
}

func (h *Hub) adcDirect(p *adc.DirectPacket, from *adcPeer) {
	peer := h.bySID(p.Targ)
	if peer == nil {
		return
	}
	if p2, ok := peer.(*adcPeer); ok {
		_ = p2.conn.WritePacket(p)
		_ = p2.conn.Flush()
		return
	}
	msg, err := p.Decode()
	if err != nil {
		log.Printf("cannot parse ADC message: %v", err)
		return
	}
	switch msg := msg.(type) {
	case adc.ChatMessage:
		h.privateChat(from, peer, string(msg.Text))
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
	default:
		// TODO: decode other packets
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
	if u.Application == "" {
		if i := strings.Index(u.Version, " "); i >= 0 {
			u.Application, u.Version = u.Version[:i], u.Version[i+1:]
		}
	}
	return User{
		Name:  u.Name,
		Share: uint64(u.ShareSize),
		Email: u.Email,
		App: Software{
			Name: u.Application,
			Vers: u.Version,
		},
		IPv4: u.Features.Has(adc.FeaTCP4),
		IPv6: u.Features.Has(adc.FeaTCP6),
		TLS:  u.Features.Has(adc.FeaADC0),
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
				Name:        info.Name,
				Id:          cid,
				Application: info.App.Name,
				Version:     info.App.Vers,
				ShareSize:   int64(info.Share),
				Email:       info.Email,
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
			if strings.HasPrefix(addr, "[") {
				u.Ip6 = addr
			} else {
				u.Ip4 = addr
			}
		}
		if err := p.conn.WriteBroadcast(peer.SID(), &u); err != nil {
			return err
		}
	}
	return p.conn.Flush()
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

func (p *adcPeer) ChatMsg(from Peer, text string) error {
	err := p.conn.WriteBroadcast(from.SID(), &adc.ChatMessage{
		Text: adc.String(text),
	})
	if err != nil {
		return err
	}
	return p.conn.Flush()
}

func (p *adcPeer) PrivateMsg(from Peer, text string) error {
	src := from.SID()
	err := p.conn.WriteDirect(src, p.sid, &adc.ChatMessage{
		Text: adc.String(text), PM: &src,
	})
	if err != nil {
		return err
	}
	return p.conn.Flush()
}

func (p *adcPeer) HubChatMsg(text string) error {
	err := p.conn.WriteInfoMsg(&adc.ChatMessage{
		Text: adc.String(text),
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

	// make sure we are on the same page - fake an update of an address for that peer
	field := [2]byte{'I', '4'} // IPv4
	if strings.HasPrefix(host, "[") {
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

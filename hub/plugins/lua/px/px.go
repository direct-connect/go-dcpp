package px

import (
	"github.com/direct-connect/go-dc/nmdc"
	"github.com/direct-connect/go-dcpp/hub"

	hlua "github.com/direct-connect/go-dcpp/hub/plugins/lua"
)

func init() {
	hlua.RegisterAPI(pxCompat{})
}

const (
	apiName = "PtokaX"

	versionString = "0.5.2.2"
	buildNumber   = "556"
)

type pxCompat struct{}

func (pxCompat) Name() string {
	return apiName
}

func (pxCompat) Version() hub.Version {
	return hub.Version{Major: 0, Minor: 1}
}

func (pxCompat) Compatible(s *hlua.Script) bool {
	return true // TODO
}

func (pxCompat) New(s *hlua.Script) hlua.Instance {
	return New(s)
}

func New(s *hlua.Script) *Script {
	ps := &Script{s: s, h: s.Hub()}
	ps.setupGlobals()
	return ps
}

type Script struct {
	s      *hlua.Script
	h      *hub.Hub
	core   core
	timers timers
}

func (s *Script) Name() string {
	return s.s.Name()
}

func (s *Script) Start() {
	if start := s.globalFunc("OnStartup", 0); start != nil {
		start.Call()
	}
	if onChat := s.globalFunc("ChatArrival", 0); onChat != nil {
		s.h.OnChat(func(p hub.Peer, m hub.Message) bool {
			u := s.luaUserArg(p, false)

			msg := hub.ToNMDCChatMsg(p, m)
			data, err := nmdc.Marshal(nil, msg)
			if err != nil {
				panic(err)
			}

			onChat.Call(u, string(data))
			return true
		})
	}
	if onJoin := s.globalFunc("UserConnected", 0); onJoin != nil {
		s.h.OnJoined(func(p hub.Peer) bool {
			u := s.luaUserArg(p, false)

			info := hub.NMDCUserInfo(p)
			data, err := nmdc.Marshal(nil, &info)
			if err != nil {
				panic(err)
			}

			onJoin.Call(u, string(data))
			return true
		})
	}
	if onLeave := s.globalFunc("UserDisconnected", 0); onLeave != nil {
		s.h.OnLeave(func(p hub.Peer) {
			u := s.luaUserArg(p, false)

			info := hub.NMDCUserInfo(p)
			data, err := nmdc.Marshal(nil, &info)
			if err != nil {
				panic(err)
			}

			onLeave.Call(u, string(data))
		})
	}
}

func (s *Script) globalFunc(name string, ret int) *hlua.Func {
	st := s.s.State()
	st.Global(name)
	if !st.IsFunction(1) {
		st.Pop(1)
		return nil
	}
	f := s.s.ToFunc(1, ret)
	st.Pop(1)
	return f
}

func (s *Script) Close() error {
	return nil // TODO
}

func (s *Script) setupGlobals() {
	s.setupCore()
	s.setupTimers()
	s.setupSettings()
	s.setupProfiles()
}

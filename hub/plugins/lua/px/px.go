package px

import (
	lua "github.com/Shopify/go-lua"
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

func (s *Script) onStartup() {
	if start := s.globalFunc("OnStartup", 0); start != nil {
		start.Call()
	}
}

func (s *Script) setupOnArrival() {
	if onChat := s.globalFunc("ChatArrival", lua.MultipleReturns); onChat != nil {
		s.h.OnGlobalChat(func(p hub.Peer, m hub.Message) bool {
			u := s.luaUserArg(p, false)

			msg := hub.ToNMDCChatMsg(p, m)
			data, err := nmdc.Marshal(nil, msg)
			if err != nil {
				panic(err)
			}

			var out bool
			onChat.CallRet(func(st *lua.State) {
				if top := st.Top(); top != 0 {
					out = st.ToBoolean(top)
				}
			}, u, string(data))
			return !out
		})
	}
}

func (s *Script) setupOnUsers() {
	onJoinUser := s.globalFunc("UserConnected", 0)
	onJoinReg := s.globalFunc("RegConnected", 0)
	onJoinOp := s.globalFunc("OpConnected", 0)
	if onJoinReg == nil && onJoinUser != nil {
		onJoinReg = onJoinUser
	}
	if onJoinOp == nil && onJoinReg != nil {
		onJoinOp = onJoinReg
	}
	if onJoinUser != nil || onJoinReg != nil || onJoinOp != nil {
		s.h.OnJoined(func(p hub.Peer) bool {
			fnc := onJoinUser
			if pr := p.User(); pr.IsOp() {
				fnc = onJoinReg
			} else if pr.IsRegistered() {
				fnc = onJoinOp
			}
			if fnc == nil {
				return true
			}

			u := s.luaUserArg(p, false)

			info := hub.NMDCUserInfo(p)
			data, err := nmdc.Marshal(nil, &info)
			if err != nil {
				panic(err)
			}
			fnc.Call(u, string(data))
			return true
		})
	}

	onLeaveUser := s.globalFunc("UserDisconnected", 0)
	onLeaveReg := s.globalFunc("RegDisconnected", 0)
	onLeaveOp := s.globalFunc("OpDisconnected", 0)
	if onLeaveReg == nil && onLeaveUser != nil {
		onLeaveReg = onLeaveUser
	}
	if onLeaveOp == nil && onLeaveReg != nil {
		onLeaveOp = onLeaveReg
	}
	if onLeaveUser != nil || onLeaveReg != nil || onLeaveOp != nil {
		s.h.OnLeave(func(p hub.Peer) {
			fnc := onLeaveUser
			if pr := p.User(); pr.IsOp() {
				fnc = onLeaveOp
			} else if pr.IsRegistered() {
				fnc = onLeaveReg
			}
			if fnc == nil {
				return
			}

			u := s.luaUserArg(p, false)

			info := hub.NMDCUserInfo(p)
			data, err := nmdc.Marshal(nil, &info)
			if err != nil {
				panic(err)
			}
			fnc.Call(u, string(data))
		})
	}
}

func (s *Script) Start() {
	s.onStartup()
	s.setupOnUsers()
	s.setupOnArrival()
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

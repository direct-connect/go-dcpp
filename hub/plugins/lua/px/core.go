package px

import (
	"log"
	"net"
	"strings"
	"sync"
	"time"

	dc "github.com/direct-connect/go-dc"
	"github.com/direct-connect/go-dc/nmdc"
	"github.com/direct-connect/go-dcpp/hub"
	hlua "github.com/direct-connect/go-dcpp/hub/plugins/lua"

	lua "github.com/Shopify/go-lua"
)

// http://wiki.ptokax.org/doku.php?id=luaapi:px_core
// https://github.com/pavel-pimenov/flylinkdc-hub/blob/master/core/LuaCoreLib.cpp

func (s *Script) setupCore() {
	strm := map[string]string{
		"Version":     versionString,
		"BuildNumber": buildNumber,
	}
	funcm := map[string]lua.Function{
		"Restart":              s.luaRestart,
		"Shutdown":             s.luaShutdown,
		"ResumeAccepts":        s.luaResumeAccepts,
		"SuspendAccepts":       s.luaSuspendAccepts,
		"RegBot":               s.luaRegBot,
		"UnregBot":             s.luaUnregBot,
		"GetBots":              s.luaGetBots,
		"GetActualUsersPeak":   s.luaGetActualUsersPeak,
		"GetMaxUsersPeak":      s.luaGetMaxUsersPeak,
		"GetCurrentSharedSize": s.luaGetCurrentSharedSize,
		"GetHubIP":             s.luaGetHubIP,
		"GetHubIPs":            s.luaGetHubIPs,
		"GetHubSecAlias":       s.luaGetHubSecAlias,
		"GetPtokaXPath":        s.luaGetPtokaXPath,
		"GetUsersCount":        s.luaGetUsersCount,
		"GetUpTime":            s.luaGetUpTime,
		"GetOnlineNonOps":      s.luaGetOnlineNonOps,
		"GetOnlineOps":         s.luaGetOnlineOps,
		"GetOnlineRegs":        s.luaGetOnlineRegs,
		"GetOnlineUsers":       s.luaGetOnlineUsers,
		//"GetUser":              s.luaGetUser,
		//"GetUserAllData":       s.luaGetUserAllData,
		//"GetUserData":          s.luaGetUserData,
		//"GetUserValue":         s.luaGetUserValue,
		//"GetUsers":             s.luaGetUsers,
		//"Disconnect":           s.luaDisconnect,
		//"Kick":                 s.luaKick,
		//"Redirect":             s.luaRedirect,
		//"DefloodWarn":          s.luaDefloodWarn,
		//"SendToAll":            s.luaSendToAll,
		//"SendToNick":           s.luaSendToNick,
		//"SendToOpChat":         s.luaSendToOpChat,
		//"SendToOps":            s.luaSendToOps,
		//"SendToProfile":        s.luaSendToProfile,
		"SendToUser": s.luaSendToUser,
		//"SendPmToAll":          s.luaSendPmToAll,
		//"SendPmToNick":         s.luaSendPmToNick,
		//"SendPmToOps":          s.luaSendPmToOps,
		//"SendPmToProfile":      s.luaSendPmToProfile,
		//"SendPmToUser":         s.luaSendPmToUser,
		//"SetUserInfo":          s.luaSetUserInfo,
	}
	m := make(map[string]interface{}, len(strm)+len(funcm))
	for k, v := range strm {
		m[k] = v
	}
	for k, v := range funcm {
		m[k] = v
	}
	s.s.Set("Core", m)
}

type core struct {
	sync.RWMutex
	bots map[string]*pxBot
}

func (s *Script) restart() {
	// TODO
	log.Print("TODO: px.Core.Restart()")
}

func (s *Script) shutdown() {
	// TODO
	log.Print("TODO: px.Core.Shutdown()")
}

func (s *Script) resumeAccepts() {
	// TODO
	log.Print("TODO: px.Core.ResumeAccepts()")
}

func (s *Script) suspendAccepts(dt time.Duration) {
	// TODO
	log.Printf("TODO: px.Core.SuspendAccepts(%v)", dt)
}

type pxBot struct {
	s *Script
	b *hub.Bot
}

func (b *pxBot) Name() string {
	return b.b.Name()
}

func (b *pxBot) IsOP() bool {
	return false // TODO
}

func (b *pxBot) Close() error {
	name := b.b.Name()
	b.s.core.Lock()
	delete(b.s.core.bots, name)
	b.s.core.Unlock()
	return b.b.Close()
}

func (s *Script) regBotName(name, desc, email string) (*pxBot, error) {
	b, err := s.h.NewBotDesc(name, desc, email, dc.Software{
		Name:    apiName,
		Version: versionString,
	})
	if err != nil {
		return nil, err
	}
	px := &pxBot{s: s, b: b}
	s.core.Lock()
	if s.core.bots == nil {
		s.core.bots = make(map[string]*pxBot)
	}
	s.core.bots[name] = px
	s.core.Unlock()
	return px, nil
}

func (s *Script) regBot(name, desc, email string, op bool) (*pxBot, error) {
	// TODO: operator flag
	b, err := s.regBotName(name, desc, email)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func (s *Script) regBotInfo(name, myinfo string, op bool) (*pxBot, error) {
	// TODO: parse MyINFO
	log.Printf("TODO: px.Core.RegBot(%q, %q, %v)", name, myinfo, op)

	b, err := s.regBotName(name, "", "")
	if err != nil {
		return nil, err
	}
	return b, nil
}

func (s *Script) unregBot(name string) bool {
	s.core.RLock()
	b, ok := s.core.bots[name]
	s.core.RUnlock()
	if !ok {
		return false
	}
	_ = b.Close()
	return true
}

func (s *Script) bots() []*pxBot {
	// TODO: should it include bots from other scripts? hub bot? op chat?
	log.Print("TODO: px.Core.GetBots()")
	var bots []*pxBot
	s.core.RLock()
	for _, b := range s.core.bots {
		bots = append(bots, b)
	}
	s.core.RUnlock()
	return bots
}

func (s *Script) actualUsersPeak() int {
	// TODO
	log.Print("TODO: px.Core.GetActualUsersPeak()")
	return 0
}

func (s *Script) maxUsersPeak() int {
	v, _ := s.getInt(px_SETSHORT_MAX_USERS_PEAK)
	return v
}

func (s *Script) currentSharedSize() uint64 {
	return s.h.Stats().Share
}

func (s *Script) hubIP() string {
	st := s.h.Stats()
	addr := st.DefaultAddr()
	if addr == "" {
		return ""
	}
	// TODO: check if this is correct
	host, _, _ := net.SplitHostPort(addr)
	log.Printf("TODO: px.Core.GetHubIP() = %q", host)
	return host
}

func (s *Script) hubIPs() []string {
	st := s.h.Stats()
	if len(st.Addr) == 0 {
		return nil
	}
	var ips []string
	for _, addr := range st.Addr {
		host, _, _ := net.SplitHostPort(addr)
		if host != "" {
			ips = append(ips, host)
		}
	}
	// TODO: check if this is correct
	log.Printf("TODO: px.Core.GetHubIPs() = %q", ips)
	return ips
}

func (s *Script) hubAliasSec() string {
	// TODO
	log.Println("TODO: px.Core.GetHubSecAlias()")
	return ""
}

func (s *Script) pxPath() string {
	// TODO
	log.Println("TODO: px.Core.GetPtokaXPath()")
	return ""
}

func (s *Script) usersCount() int {
	return s.h.Stats().Users
}

func (s *Script) uptimeSec() uint {
	return uint(s.h.Uptime().Seconds())
}

func (s *Script) onlineFilter(filter func(p hub.Peer) bool) []hub.Peer {
	var out []hub.Peer
	for _, p := range s.h.Peers() {
		if filter == nil || filter(p) {
			out = append(out, p)
		}
	}
	return out
}

func (s *Script) luaRestart(st *lua.State) int {
	if !noArgs(st, "Restart") {
		return 0
	}
	s.restart()
	return 0
}

func (s *Script) luaShutdown(st *lua.State) int {
	if !noArgs(st, "Shutdown") {
		return 0
	}
	s.restart()
	return 0
}

func (s *Script) luaResumeAccepts(st *lua.State) int {
	if !noArgs(st, "ResumeAccepts") {
		return 0
	}
	s.resumeAccepts()
	return 0
}

func (s *Script) luaSuspendAccepts(st *lua.State) int {
	switch n := st.Top(); n {
	case 0:
		if !noArgs(st, "SuspendAccepts") {
			return 0
		}
		s.suspendAccepts(0)
		return 0
	case 1:
		if !assertArgsT(st, "SuspendAccepts", lua.TypeNumber) {
			return 0
		}
		sec, _ := st.ToNumber(1)
		st.SetTop(0)
		if sec != 0 {
			s.suspendAccepts(time.Duration(sec * float64(time.Second)))
		}
		return 0
	default:
		lua.Errorf(st, "bad argument count to 'SuspendAccepts' (0 or 1 expected, got %d)", n)
		st.SetTop(0)
		return 0
	}
}

func (s *Script) luaRegBot(st *lua.State) int {
	n := st.Top()
	if n != 4 && n != 3 {
		lua.Errorf(st, "bad argument count to 'RegBot' (3 or 4 expected, got %d)", n)
		st.SetTop(0)
		st.PushNil()
		return 1
	}
	var (
		bot *pxBot
		err error
	)
	if n == 4 {
		if !assertArgsT(st, "RegBot", lua.TypeString, lua.TypeString, lua.TypeString, lua.TypeBoolean) {
			st.PushNil()
			return 1
		}
		name, _ := st.ToString(1)
		desc, _ := st.ToString(2)
		email, _ := st.ToString(3)
		isOP := st.ToBoolean(4)
		st.SetTop(0)

		bot, err = s.regBot(name, desc, email, isOP)
	} else {
		if !assertArgsT(st, "RegBot", lua.TypeString, lua.TypeString, lua.TypeBoolean) {
			st.PushNil()
			return 1
		}
		name, _ := st.ToString(1)
		myinfo, _ := st.ToString(2)
		isOP := st.ToBoolean(4)
		st.SetTop(0)

		bot, err = s.regBotInfo(name, myinfo, isOP)
	}
	if err != nil {
		lua.Errorf(st, "%v", err)
		st.PushNil()
		return 1
	} else if bot == nil {
		st.PushNil()
		return 1
	}
	st.PushBoolean(true)
	return 1
}

func (s *Script) luaUnregBot(st *lua.State) int {
	if !assertArgsT(st, "UnregBot", lua.TypeString) {
		st.PushNil()
		return 1
	}
	name, _ := st.ToString(1)
	st.SetTop(0)

	if !s.unregBot(name) {
		st.PushNil()
		return 1
	}
	st.PushBoolean(true)
	return 1
}

func (s *Script) luaGetBots(st *lua.State) int {
	if !noArgs(st, "GetBots") {
		st.PushNil()
		return 0
	}

	st.NewTable()
	t := st.Top()

	for i, bot := range s.bots() {
		st.PushInteger(i + 1)

		st.NewTable()
		b := st.Top()

		st.PushString("sNick")
		st.PushString(bot.Name())
		st.RawSet(b)

		st.PushString("sMyINFO")
		st.PushNil() // FIXME
		st.RawSet(b)

		st.PushString("bIsOP")
		if bot.IsOP() {
			st.PushBoolean(true)
		} else {
			st.PushNil()
		}
		st.RawSet(b)

		st.PushString("sScriptName")
		st.PushString(s.Name()) // TODO: relative or abs? relative to what?
		st.RawSet(b)

		st.RawSet(t)
	}

	return 1
}

func (s *Script) luaGetActualUsersPeak(st *lua.State) int {
	if !noArgs(st, "GetActualUsersPeak") {
		st.PushNil()
		return 1
	}
	v := s.actualUsersPeak()
	st.PushInteger(v)
	return 1
}

func (s *Script) luaGetMaxUsersPeak(st *lua.State) int {
	if !noArgs(st, "GetMaxUsersPeak") {
		st.PushNil()
		return 1
	}
	v := s.maxUsersPeak()
	st.PushInteger(v)
	return 1
}

func (s *Script) luaGetCurrentSharedSize(st *lua.State) int {
	if !noArgs(st, "GetCurrentSharedSize") {
		st.PushNil()
		return 1
	}
	v := s.currentSharedSize()
	st.PushUnsigned(uint(v))
	return 1
}

func (s *Script) luaGetHubIP(st *lua.State) int {
	if !noArgs(st, "GetHubIP") {
		st.PushNil()
		return 1
	}
	v := s.hubIP()
	if v != "" {
		st.PushString(v)
	} else {
		st.PushNil()
	}
	return 1
}

func (s *Script) luaGetHubIPs(st *lua.State) int {
	if !noArgs(st, "GetHubIPs") {
		st.PushNil()
		return 1
	}
	ips := s.hubIPs()
	if len(ips) == 0 {
		st.PushNil()
		return 1
	}

	st.NewTable()
	t := st.Top()

	// TODO: seems like IPv6 should be first
	for i, ip := range ips {
		st.PushInteger(i + 1)
		st.PushString(ip)
		st.RawSet(t)
	}

	return 1
}

func (s *Script) luaGetHubSecAlias(st *lua.State) int {
	if !noArgs(st, "GetHubSecAlias") {
		st.PushNil()
		return 1
	}
	v := s.hubAliasSec()
	st.PushString(v)
	return 1
}

func (s *Script) luaGetPtokaXPath(st *lua.State) int {
	if !noArgs(st, "GetPtokaXPath") {
		st.PushNil()
		return 1
	}
	v := s.pxPath()
	st.PushString(v)
	return 1
}

func (s *Script) luaGetUsersCount(st *lua.State) int {
	if !noArgs(st, "GetUsersCount") {
		st.PushNil()
		return 1
	}
	v := s.usersCount()
	st.PushInteger(v)
	return 1
}

func (s *Script) luaGetUpTime(st *lua.State) int {
	if !noArgs(st, "GetUpTime") {
		st.PushNil()
		return 1
	}
	v := s.uptimeSec()
	st.PushUnsigned(v)
	return 1
}

func (s *Script) luaUserArg(p hub.Peer, full bool) interface{} {
	if p == nil {
		return nil
	}
	// FIXME: other fields
	name := p.Name()
	iprof := -1
	if u := p.User(); u != nil {
		// TODO: check if this is valid
		if u.IsOwner() {
			iprof = 0
		} else if u.IsOp() {
			iprof = 1
		} else if u.IsRegistered() {
			iprof = 3
		}
	}
	return hlua.M{
		"uptr":     hlua.UserData{Ptr: p},
		"sName":    name,
		"sNick":    name,
		"iProfile": iprof,
	}
}

func (s *Script) luaPushUser(st *lua.State, u hub.Peer, full bool) {
	m := s.luaUserArg(u, full)
	s.s.Push(m)
}

func (s *Script) luaAsUser(st *lua.State, at int, fname string) hub.Peer {
	top := st.Top()
	st.PushString("uptr")
	st.RawGet(at)

	if st.Top() != top+1 || st.TypeOf(-1) != lua.TypeUserData {
		lua.Errorf(st, "bad argument #1 to '%s' (it's not user table)", fname)
		return nil
	}
	u := st.ToUserData(-1)
	st.Pop(1)
	if u == nil {
		return nil
	}
	return u.(hub.Peer)
}

func (s *Script) luaGetOnlineFilter(st *lua.State, fname string, filter func(p hub.Peer) bool) int {
	n := st.Top()

	full := false
	if n != 0 {
		if n != 1 {
			lua.Errorf(st, "bad argument count to '%s' (0 or 1 expected, got %d)", fname, n)
			st.SetTop(0)
			st.PushNil()
			return 1
		}
		// n == 1
		if !assertArgsT(st, fname, lua.TypeBoolean) {
			st.PushNil()
			return 1
		}
		full = st.ToBoolean(1)
		st.SetTop(0)
	}

	st.NewTable()
	t := st.Top()

	for i, u := range s.onlineFilter(filter) {
		st.PushInteger(i + 1)
		s.luaPushUser(st, u, full)
		st.RawSet(t)
	}

	return 1
}

func (s *Script) luaGetOnlineNonOps(st *lua.State) int {
	return s.luaGetOnlineFilter(st, "GetOnlineNonOps", func(p hub.Peer) bool {
		return !p.User().IsOp()
	})
}

func (s *Script) luaGetOnlineOps(st *lua.State) int {
	return s.luaGetOnlineFilter(st, "GetOnlineOps", func(p hub.Peer) bool {
		return p.User().IsOp()
	})
}

func (s *Script) luaGetOnlineRegs(st *lua.State) int {
	return s.luaGetOnlineFilter(st, "GetOnlineRegs", func(p hub.Peer) bool {
		return p.User().IsRegistered()
	})
}

func (s *Script) luaGetOnlineUsers(st *lua.State) int {
	n := st.Top()

	full := false
	profile := -2
	if n == 2 {
		if !assertArgsT(st, "GetOnlineUsers", lua.TypeNumber, lua.TypeBoolean) {
			st.PushNil()
			return 1
		}
		profile, _ = st.ToInteger(1)
		full = st.ToBoolean(2)
		st.SetTop(0)
	} else if n == 1 {
		switch tp := st.TypeOf(1); tp {
		case lua.TypeNumber:
			profile, _ = st.ToInteger(1)
		case lua.TypeBoolean:
			full = st.ToBoolean(1)
		default:
			lua.Errorf(st, "bad argument #1 to 'GetOnlineUsers' (number or boolean expected, got %s)", lua.TypeNameOf(st, 1))
			st.SetTop(0)
			st.PushNil()
			return 1
		}
		st.SetTop(0)
	} else if n != 0 {
		lua.Errorf(st, "bad argument count to 'GetOnlineUsers' (0, 1 or 2 expected, got %d)", n)
		st.SetTop(0)
		st.PushNil()
		return 1
	}

	st.NewTable()
	t := st.Top()

	var filter func(hub.Peer) bool
	if profile != -2 {
		// TODO: more filters
		switch profile {
		case -1: // unregistered
			filter = func(p hub.Peer) bool {
				return !p.User().IsRegistered()
			}
		case 0: // Master
			filter = func(p hub.Peer) bool {
				return p.User().IsOwner()
			}
		case 1: // Operator
			filter = func(p hub.Peer) bool {
				return p.User().IsOp()
			}
		case 3: // Registered
			filter = func(p hub.Peer) bool {
				return p.User().IsRegistered()
			}
		}
	}
	for i, u := range s.onlineFilter(filter) {
		st.PushInteger(i + 1)
		s.luaPushUser(st, u, full)
		st.RawSet(t)
	}

	return 1
}

//func (s *Script) luaGetUser(st *lua.State) int {
//}
//
//func (s *Script) luaGetUserAllData(st *lua.State) int {
//}
//
//func (s *Script) luaGetUserData(st *lua.State) int {
//}
//
//func (s *Script) luaGetUserValue(st *lua.State) int {
//}
//
//func (s *Script) luaGetUsers(st *lua.State) int {
//}
//
//func (s *Script) luaDisconnect(st *lua.State) int {
//}
//
//func (s *Script) luaKick(st *lua.State) int {
//}
//
//func (s *Script) luaRedirect(st *lua.State) int {
//}
//
//func (s *Script) luaDefloodWarn(st *lua.State) int {
//}
//
//func (s *Script) luaSendToAll(st *lua.State) int {
//}
//
//func (s *Script) luaSendToNick(st *lua.State) int {
//}
//
//func (s *Script) luaSendToOpChat(st *lua.State) int {
//}
//
//func (s *Script) luaSendToOps(st *lua.State) int {
//}
//
//func (s *Script) luaSendToProfile(st *lua.State) int {
//}

func (s *Script) luaSendToUser(st *lua.State) int {
	if !assertArgsT(st, "SendToUser", lua.TypeTable, lua.TypeString) {
		return 0
	}
	p := s.luaAsUser(st, 1, "SendToUser")
	cmd, _ := st.ToString(2)
	st.SetTop(0)
	if p == nil {
		return 0
	}
	if !strings.HasSuffix(cmd, "|") {
		cmd += "|"
	}
	m, err := nmdc.Unmarshal(nil, []byte(cmd))
	if err != nil {
		lua.Errorf(st, "failed to decode message: %v (%s)", err, cmd)
		return 0
	}
	err = s.h.SendNMDCTo(p, m)
	if err != nil {
		lua.Errorf(st, "%s", err.Error())
	}
	return 0
}

//func (s *Script) luaSendPmToAll(st *lua.State) int {
//}
//
//func (s *Script) luaSendPmToNick(st *lua.State) int {
//}
//
//func (s *Script) luaSendPmToOps(st *lua.State) int {
//}
//
//func (s *Script) luaSendPmToProfile(st *lua.State) int {
//}
//
//func (s *Script) luaSendPmToUser(st *lua.State) int {
//}
//
//func (s *Script) luaSetUserInfo(st *lua.State) int {
//}

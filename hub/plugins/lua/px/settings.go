package px

import (
	"strings"

	"github.com/direct-connect/go-dcpp/hub"

	lua "github.com/Shopify/go-lua"
)

// http://wiki.ptokax.org/doku.php?id=luaapi:px_setman
// https://github.com/pavel-pimenov/flylinkdc-hub/blob/master/core/LuaSetManLib.cpp

func (s *Script) setupSettings() {
	s.s.SetRawFuncMap("SetMan", map[string]lua.Function{
		"Save":            s.luaSettingsSave,
		"GetMOTD":         s.luaGetMOTD,
		"SetMOTD":         s.luaSetMOTD,
		"GetBool":         s.luaGetBool,
		"SetBool":         s.luaSetBool,
		"GetNumber":       s.luaGetNumber,
		"SetNumber":       s.luaSetNumber,
		"GetString":       s.luaGetString,
		"SetString":       s.luaSetString,
		"GetMinShare":     s.luaGetMinShare,
		"SetMinShare":     s.luaSetMinShare,
		"GetMaxShare":     s.luaGetMaxShare,
		"SetMaxShare":     s.luaSetMaxShare,
		"SetHubSlotRatio": s.luaSetHubSlotRatio,
		"GetOpChat":       s.luaGetOpChat,
		"SetOpChat":       s.luaSetOpChat,
		"GetHubBot":       s.luaGetHubBot,
		"SetHubBot":       s.luaSetHubBot,
	})
}

func (s *Script) saveSettings() {
	// TODO
	s.h.Log("TODO: px.SetMan.Save()")
}

func (s *Script) getMOTD() string {
	motd, _ := s.h.GetConfigString(hub.ConfigHubMOTD)
	return motd
}

func (s *Script) setMOTD(motd string) {
	s.h.SetConfigString(hub.ConfigHubMOTD, motd)
}

func (s *Script) getBool(id idBool) (bool, bool) {
	switch id {
	default:
		// TODO
		s.h.Logf("TODO: px.SetMan.GetBool(%d)", id)
	}
	return false, false
}

func (s *Script) setBool(id idBool, v bool) {
	switch id {
	default:
		// TODO
		s.h.Logf("TODO: px.SetMan.SetBool(%d, %v)", id, v)
	}
}

func (s *Script) getInt(id idInt) (int, bool) {
	switch id {
	default:
		// TODO
		s.h.Logf("TODO: px.SetMan.GetNumber(%d)", id)
	}
	return 0, false
}

func (s *Script) setInt(id idInt, v int) {
	switch id {
	default:
		// TODO
		s.h.Logf("TODO: px.SetMan.SetNumber(%d, %d)", id, v)
	}
}

func (s *Script) getString(id idString) (string, bool) {
	switch id {
	case px_SETTXT_HUB_NAME:
		return s.h.GetConfigString(hub.ConfigHubName)
	case px_SETTXT_HUB_DESCRIPTION:
		return s.h.GetConfigString(hub.ConfigHubDesc)
	case px_SETTXT_HUB_TOPIC:
		return s.h.GetConfigString(hub.ConfigHubTopic)
	case px_SETTXT_HUB_OWNER_EMAIL:
		return s.h.GetConfigString(hub.ConfigHubEmail)
	case px_SETTXT_HUB_ADDRESS:
		st := s.h.Stats()
		addr := st.DefaultAddr()
		return addr, addr != ""
	case px_SETTXT_BOT_NICK:
		b := s.h.HubUser()
		return b.Name(), true
	case px_SETTXT_BOT_DESCRIPTION:
		u := s.h.HubUser().UserInfo()
		return u.Desc, true
	case px_SETTXT_BOT_EMAIL:
		u := s.h.HubUser().UserInfo()
		return u.Email, true
	case px_SETTXT_ENCODING:
		enc := s.h.Stats().Enc
		return enc, enc != ""
	case px_SETTXT_CHAT_COMMANDS_PREFIXES:
		return "!+/", true
	default:
		// TODO
		s.h.Logf("TODO: px.SetMan.GetString(%d)", id)
	}
	return "", false
}

func (s *Script) setString(id idString, v string) {
	switch id {
	case px_SETTXT_HUB_NAME:
		s.h.SetConfigString(hub.ConfigHubName, v)
	case px_SETTXT_HUB_DESCRIPTION:
		s.h.SetConfigString(hub.ConfigHubDesc, v)
	case px_SETTXT_HUB_TOPIC:
		s.h.SetConfigString(hub.ConfigHubTopic, v)
	case px_SETTXT_HUB_OWNER_EMAIL:
		s.h.SetConfigString(hub.ConfigHubEmail, v)
	default:
		// TODO
		s.h.Logf("TODO: px.SetMan.SetString(%d, %q)", id, v)
	}
}

func (s *Script) getMinShare() uint64 {
	// TODO
	s.h.Log("TODO: px.SetMan.GetMinShare()")
	return 0
}

func (s *Script) setMinShare(v uint64) {
	// TODO
	s.h.Logf("TODO: px.SetMan.SetMinShare(%d)", v)
}

func (s *Script) getMaxShare() uint64 {
	// TODO
	s.h.Log("TODO: px.SetMan.GetMaxShare()")
	return 0
}

func (s *Script) setMaxShare(v uint64) {
	// TODO
	s.h.Logf("TODO: px.SetMan.SetMaxShare(%d)", v)
}

func (s *Script) setHubsSlotsRatio(hubs, slots uint) {
	// TODO
	s.h.Logf("TODO: px.SetMan.SetHubSlotRatio(%d, %d)", hubs, slots)
}

func (s *Script) luaSettingsSave(st *lua.State) int {
	if !noArgs(st, "Save") {
		return 0
	}
	s.saveSettings()
	return 0
}

func (s *Script) luaGetMOTD(st *lua.State) int {
	if !noArgs(st, "GetMOTD") {
		st.PushNil()
		return 1
	}
	motd := s.getMOTD()
	st.PushString(motd)
	return 1
}

func (s *Script) luaSetMOTD(st *lua.State) int {
	if !assertArgsT(st, "SetMOTD", lua.TypeString) {
		return 0
	}
	motd, _ := st.ToString(1)
	s.setMOTD(motd)
	st.SetTop(0)
	return 0
}

func (s *Script) luaGetBool(st *lua.State) int {
	if !assertArgsT(st, "GetBool", lua.TypeNumber) {
		st.PushNil()
		return 0
	}
	id, _ := st.ToUnsigned(1)
	if id >= uint(px_SETBOOL_IDS_END) {
		lua.Errorf(st, "bad argument #1 to 'GetBool' (it's not valid id)")
		st.SetTop(0)
		st.PushNil()
		return 1
	}
	v, ok := s.getBool(idBool(id))
	if !ok {
		st.PushNil()
	} else {
		st.PushBoolean(v)
	}
	return 1
}

func (s *Script) luaSetBool(st *lua.State) int {
	if !assertArgsT(st, "SetBool", lua.TypeNumber, lua.TypeBoolean) {
		return 0
	}
	id, _ := st.ToUnsigned(1)
	v := st.ToBoolean(2)
	st.SetTop(0)

	if id >= uint(px_SETBOOL_IDS_END) {
		lua.Errorf(st, "bad argument #1 to 'SetBool' (it's not valid id)")
		return 0
	}
	s.setBool(idBool(id), v)
	return 0
}

func (s *Script) luaGetNumber(st *lua.State) int {
	if !assertArgsT(st, "GetNumber", lua.TypeNumber) {
		st.PushNil()
		return 0
	}
	id, _ := st.ToUnsigned(1)
	if id >= uint(px_SETSHORT_IDS_END) {
		lua.Errorf(st, "bad argument #1 to 'GetNumber' (it's not valid id)")
		st.SetTop(0)
		st.PushNil()
		return 1
	}
	v, ok := s.getInt(idInt(id))
	if !ok {
		st.PushNil()
	} else {
		st.PushInteger(v)
	}
	return 1
}

func (s *Script) luaSetNumber(st *lua.State) int {
	if !assertArgsT(st, "SetNumber", lua.TypeNumber, lua.TypeNumber) {
		return 0
	}
	id, _ := st.ToUnsigned(1)
	v, _ := st.ToInteger(2)
	st.SetTop(0)

	if id >= uint(px_SETSHORT_IDS_END) {
		lua.Errorf(st, "bad argument #1 to 'SetNumber' (it's not valid id)")
		return 0
	}
	s.setInt(idInt(id), v)
	return 0
}

func (s *Script) luaGetString(st *lua.State) int {
	if !assertArgsT(st, "GetString", lua.TypeNumber) {
		st.PushNil()
		return 0
	}
	id, _ := st.ToUnsigned(1)
	if id >= uint(px_SETTXT_IDS_END) {
		lua.Errorf(st, "bad argument #1 to 'GetString' (it's not valid id)")
		st.SetTop(0)
		st.PushNil()
		return 1
	}
	v, ok := s.getString(idString(id))
	if !ok {
		st.PushNil()
	} else {
		st.PushString(v)
	}
	return 1
}

func (s *Script) luaSetString(st *lua.State) int {
	if !assertArgsT(st, "SetString", lua.TypeNumber, lua.TypeString) {
		return 0
	}
	id, _ := st.ToUnsigned(1)
	v, _ := st.ToString(2)
	st.SetTop(0)

	if id >= uint(px_SETTXT_IDS_END) {
		lua.Errorf(st, "bad argument #1 to 'SetString' (it's not valid id)")
		return 0
	}
	s.setString(idString(id), v)
	return 0
}

func (s *Script) luaGetMinShare(st *lua.State) int {
	if !noArgs(st, "GetMinShare") {
		st.PushNil()
		return 1
	}
	v := s.getMinShare()
	st.PushUnsigned(uint(v))
	return 1
}

func (s *Script) luaSetBytes(st *lua.State, fname string) (uint64, bool) {
	switch n := st.Top(); n {
	case 1:
		// size in bytes
		if !assertArgsT(st, fname, lua.TypeNumber) {
			return 0, false
		}
		v, _ := st.ToUnsigned(1)
		st.SetTop(0)
		return uint64(v), true
	case 2:
		// size and units
		if !assertArgsT(st, fname, lua.TypeNumber, lua.TypeNumber) {
			return 0, false
		}
		v, _ := st.ToNumber(1)
		unit, _ := st.ToUnsigned(2)
		st.SetTop(0)

		var mult uint64 = 1
		mult <<= unit * 10

		b := uint64(float64(mult) * v)

		return b, true
	default:
		lua.Errorf(st, "bad argument count to '%s' (1 or 2 expected, got %d)", fname, n)
		st.SetTop(0)
		return 0, false
	}
}

func (s *Script) luaSetMinShare(st *lua.State) int {
	v, ok := s.luaSetBytes(st, "SetMinShare")
	if !ok {
		return 0
	}
	s.setMinShare(v)
	return 0
}

func (s *Script) luaGetMaxShare(st *lua.State) int {
	if !noArgs(st, "GetMaxShare") {
		st.PushNil()
		return 1
	}
	v := s.getMaxShare()
	st.PushUnsigned(uint(v))
	return 1
}

func (s *Script) luaSetMaxShare(st *lua.State) int {
	v, ok := s.luaSetBytes(st, "SetMaxShare")
	if !ok {
		return 0
	}
	s.setMaxShare(v)
	return 0
}

func (s *Script) luaSetHubSlotRatio(st *lua.State) int {
	if !assertArgsT(st, "SetHubSlotRatio", lua.TypeNumber, lua.TypeNumber) {
		return 0
	}
	hubs, _ := st.ToUnsigned(1)
	slots, _ := st.ToUnsigned(2)
	st.SetTop(0)
	s.setHubsSlotsRatio(hubs, slots)
	return 0
}

func (s *Script) luaGetOpChat(st *lua.State) int {
	if !noArgs(st, "GetOpChat") {
		st.PushNil()
		return 1
	}
	st.NewTable()
	i := st.Top()

	for _, v := range []struct {
		key string
		id  idString
	}{
		{"sNick", px_SETTXT_OP_CHAT_NICK},
		{"sDescription", px_SETTXT_OP_CHAT_DESCRIPTION},
		{"sEmail", px_SETTXT_OP_CHAT_EMAIL},
	} {
		st.PushString(v.key)
		if name, ok := s.getString(v.id); !ok {
			st.PushNil()
		} else {
			st.PushString(name)
		}
		st.RawSet(i)
	}

	st.PushString("bEnabled")
	if v, _ := s.getBool(px_SETBOOL_REG_OP_CHAT); v {
		st.PushBoolean(v)
	} else {
		st.PushNil()
	}
	st.RawSet(i)

	return 1
}

func (s *Script) luaSetOpChat(st *lua.State) int {
	if !assertArgsT(st, "SetOpChat", lua.TypeBoolean, lua.TypeString, lua.TypeString, lua.TypeString) {
		return 0
	}
	enable := st.ToBoolean(1)
	name, _ := st.ToString(2)
	desc, _ := st.ToString(3)
	email, _ := st.ToString(4)
	st.SetTop(0)
	// TODO: move to setString
	if len(name) == 0 ||
		len(name) > 64 || len(desc) > 64 || len(email) > 64 ||
		strings.ContainsAny(name, "$|") ||
		strings.ContainsAny(desc, "$|") ||
		strings.ContainsAny(email, "$|") {
		return 0
	}
	s.setBool(px_SETBOOL_REG_OP_CHAT, enable)
	s.setString(px_SETTXT_OP_CHAT_NICK, name)
	s.setString(px_SETTXT_OP_CHAT_DESCRIPTION, desc)
	s.setString(px_SETTXT_OP_CHAT_EMAIL, email)
	return 0
}

func (s *Script) luaGetHubBot(st *lua.State) int {
	if !noArgs(st, "GetHubBot") {
		st.PushNil()
		return 1
	}
	st.NewTable()
	i := st.Top()

	for _, v := range []struct {
		key string
		id  idString
	}{
		{"sNick", px_SETTXT_BOT_NICK},
		{"sDescription", px_SETTXT_BOT_DESCRIPTION},
		{"sEmail", px_SETTXT_BOT_EMAIL},
	} {
		st.PushString(v.key)
		if name, ok := s.getString(v.id); !ok {
			st.PushNil()
		} else {
			st.PushString(name)
		}
		st.RawSet(i)
	}

	for _, v := range []struct {
		key string
		id  idBool
	}{
		{"bEnabled", px_SETBOOL_REG_BOT},
		{"bUsedAsHubSecAlias", px_SETBOOL_USE_BOT_NICK_AS_HUB_SEC},
	} {
		st.PushString(v.key)
		if v, _ := s.getBool(v.id); v {
			st.PushBoolean(v)
		} else {
			st.PushNil()
		}
		st.RawSet(i)
	}

	return 1
}

func (s *Script) luaSetHubBot(st *lua.State) int {
	if !assertArgsT(st, "SetHubBot", lua.TypeBoolean, lua.TypeString, lua.TypeString, lua.TypeString, lua.TypeBoolean) {
		return 0
	}
	enable := st.ToBoolean(1)
	name, _ := st.ToString(2)
	desc, _ := st.ToString(3)
	email, _ := st.ToString(4)
	hsec := st.ToBoolean(5)
	st.SetTop(0)
	// TODO: move to setString
	if len(name) == 0 ||
		len(name) > 64 || len(desc) > 64 || len(email) > 64 ||
		strings.ContainsAny(name, "$|") ||
		strings.ContainsAny(desc, "$|") ||
		strings.ContainsAny(email, "$|") {
		return 0
	}
	s.setBool(px_SETBOOL_REG_BOT, enable)
	s.setString(px_SETTXT_BOT_NICK, name)
	s.setString(px_SETTXT_BOT_DESCRIPTION, desc)
	s.setString(px_SETTXT_BOT_EMAIL, email)
	s.setBool(px_SETBOOL_USE_BOT_NICK_AS_HUB_SEC, hsec)
	return 0
}

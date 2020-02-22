package px

import (
	"github.com/direct-connect/go-dcpp/hub"
	hlua "github.com/direct-connect/go-dcpp/hub/plugins/lua"

	lua "github.com/Shopify/go-lua"
)

// http://wiki.ptokax.org/doku.php?id=luaapi:px_profman
// https://github.com/pavel-pimenov/flylinkdc-hub/blob/master/core/LuaProfManLib.cpp

func (s *Script) setupProfiles() {
	s.s.SetRawFuncMap("ProfMan", map[string]lua.Function{
		"Save": s.luaProfilesSave,
		//"AddProfile":            s.luaAddProfile,
		//"RemoveProfile":         s.luaRemoveProfile,
		//"MoveDown":              s.luaMoveDown,
		//"MoveUp":                s.luaMoveUp,
		"GetProfile": s.luaGetProfile,
		//"GetProfiles":           s.luaGetProfiles,
		//"GetProfilePermission":  s.luaGetProfilePermission,
		//"GetProfilePermissions": s.luaGetProfilePermissions,
		//"SetProfileName":        s.luaSetProfileName,
		//"SetProfilePermission":  s.luaSetProfilePermission,
	})
}

func (s *Script) saveProfiles() {
	// TODO
	s.h.Log("TODO: px.ProfMan.Save()")
}

type pxProfile struct {
	id int
	u  *hub.UserProfile
}

func (s *Script) profileByName(name string) *pxProfile {
	if name == "" {
		return nil
	}
	u := s.h.Profile(name)
	if u == nil {
		return nil
	}
	id := 4 // FIXME
	switch name {
	case hub.ProfileNameRoot:
		id = 0
	case hub.ProfileNameOperator:
		id = 1
	case hub.ProfileNameRegistered:
		id = 3
	}
	return &pxProfile{id: id, u: u}
}

func (s *Script) profileByID(id int) *pxProfile {
	if id < 0 {
		return nil // unregistered
	}
	switch id {
	case 0: // Master
		u := s.h.Profile(hub.ProfileNameRoot)
		return &pxProfile{id: id, u: u}
	case 1: // Operator
		u := s.h.Profile(hub.ProfileNameOperator)
		return &pxProfile{id: id, u: u}
	case 3: // Registered
		u := s.h.Profile(hub.ProfileNameRegistered)
		return &pxProfile{id: id, u: u}
	}
	return nil // FIXME
}

func (s *Script) luaProfilesSave(st *lua.State) int {
	if !noArgs(st, "Save") {
		return 0
	}
	s.saveProfiles()
	return 0
}

func (s *Script) luaPushProfile(st *lua.State, p *pxProfile) {
	if p == nil {
		st.PushNil()
		return
	}
	// TODO: tProfilePermissions field
	s.s.Push(hlua.M{
		"iProfileNumber":      p.id,
		"sProfileName":        p.u.ID(),
		"tProfilePermissions": hlua.M{},
	})
}

func (s *Script) luaGetProfile(st *lua.State) int {
	if !assertArgsN(st, "GetProfile", 1) {
		st.PushNil()
		return 1
	}
	switch st.TypeOf(1) {
	case lua.TypeString:
		name, _ := st.ToString(1)
		st.SetTop(0)
		p := s.profileByName(name)
		s.luaPushProfile(st, p)
		return 1
	case lua.TypeNumber:
		id, _ := st.ToInteger(1)
		st.SetTop(0)
		p := s.profileByID(id)
		s.luaPushProfile(st, p)
		return 1
	}
	lua.Errorf(st, "bad argument #1 to 'GetProfile' (string or number expected, got %v)", lua.TypeNameOf(st, 1))
	st.SetTop(0)
	st.PushNil()
	return 1
}

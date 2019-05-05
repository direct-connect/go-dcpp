package px

import (
	"sync"
	"time"

	lua "github.com/Shopify/go-lua"
	hlua "github.com/direct-connect/go-dcpp/hub/plugins/lua"
)

// http://wiki.ptokax.org/doku.php?id=luaapi:px_tmrman
// https://github.com/pavel-pimenov/flylinkdc-hub/blob/master/core/LuaTmrManLib.cpp

func (s *Script) setupTimers() {
	s.s.SetRawFuncMap("TmrMan", map[string]lua.Function{
		"AddTimer":    s.luaAddTimer,
		"RemoveTimer": s.luaRemoveTimer,
	})
}

type timers struct {
	sync.RWMutex
	m map[*pxTimer]struct{}
}

type pxTimer struct {
	s      *Script
	dt     time.Duration
	ticker *time.Ticker
}

func (t *pxTimer) Start(fnc func(*pxTimer)) {
	t.ticker = time.NewTicker(t.dt)
	go func() {
		for range t.ticker.C {
			fnc(t)
		}
	}()
}

func (t *pxTimer) Stop() {
	t.ticker.Stop()
	t.s.timers.Lock()
	delete(t.s.timers.m, t)
	t.s.timers.Unlock()
}

func (s *Script) setTimer(dt time.Duration) *pxTimer {
	s.timers.Lock()
	defer s.timers.Unlock()
	t := &pxTimer{s: s, dt: dt}
	if s.timers.m == nil {
		s.timers.m = make(map[*pxTimer]struct{})
	}
	s.timers.m[t] = struct{}{}
	return t
}

func (s *Script) luaAddTimer(st *lua.State) int {
	n := st.Top()
	if n < 1 || n > 2 {
		lua.Errorf(st, "bad argument count to 'AddTimer' (1 or 2 expected, got %d)", n)
		st.SetTop(0)
		st.PushNil()
		return 1
	}
	if st.TypeOf(1) != lua.TypeNumber {
		lua.CheckNumber(st, 1)
		st.SetTop(0)
		st.PushNil()
		return 1
	}
	ms, ok := st.ToNumber(1)
	if !ok {
		st.SetTop(0)
		st.PushNil()
		return 1
	}
	t := s.setTimer(time.Duration(ms * float64(time.Millisecond)))

	var (
		fnc     *hlua.Func
		fncName string
	)
	if n < 2 {
		fncName = "OnTimer"
	} else {
		switch st.TypeOf(2) {
		case lua.TypeFunction:
			fnc = s.s.ToFuncOn(st, 2, 0)
		case lua.TypeString:
			v, ok := st.ToString(2)
			if !ok {
				st.SetTop(0)
				st.PushNil()
				return 1
			}
			fncName = v
		default:
			lua.Errorf(st, "bad argument #2 to 'AddTimer' (string or function expected, got %s)", lua.TypeNameOf(st, 2))
			st.SetTop(0)
			st.PushNil()
			return 1
		}
	}
	if fncName != "" {
		st.Global(fncName)
		i := st.Top()
		if !st.IsFunction(i) {
			st.SetTop(0)
			st.PushNil()
			return 1
		}
		fnc = s.s.ToFuncOn(st, i, 0)
	}
	t.Start(func(tm *pxTimer) {
		fnc.Call(hlua.UserData{Ptr: tm})
	})
	s.s.Push(hlua.UserData{Ptr: t})
	return 1
}

func (s *Script) luaRemoveTimer(st *lua.State) int {
	if n := st.Top(); n != 1 {
		lua.Errorf(st, "bad argument count to 'RemoveTimer' (1 expected, got %d)", n)
		st.SetTop(0)
		return 0
	}
	if st.TypeOf(1) != lua.TypeUserData {
		lua.CheckType(st, 1, lua.TypeUserData)
		st.SetTop(0)
		return 0
	}
	t, ok := st.ToUserData(1).(*pxTimer)
	if ok {
		t.Stop()
	}
	st.SetTop(0)
	return 0
}

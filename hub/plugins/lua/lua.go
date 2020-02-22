package lua

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	lua "github.com/Shopify/go-lua"

	"github.com/direct-connect/go-dcpp/hub"
)

const apiVersion = "0.1.0"

func init() {
	hub.RegisterPlugin(&plugin{})
}

var (
	apis []API
)

func RegisterAPI(api API) {
	apis = append(apis, api)
}

type Instance interface {
	Start()
	Close() error
}

type API interface {
	Name() string
	Version() hub.Version
	Compatible(s *Script) bool
	New(s *Script) Instance
}

type plugin struct {
	h *hub.Hub

	scripts map[string]*Script
}

func (p *plugin) Name() string {
	return "LUA"
}

func (p *plugin) Version() hub.Version {
	return hub.Version{Major: 0, Minor: 2}
}

func (p *plugin) Init(h *hub.Hub, path string) error {
	p.h = h
	return p.loadScripts(path)
}

func (p *plugin) loadScripts(path string) error {
	p.scripts = make(map[string]*Script)

	path = filepath.Join(path, "scripts")
	p.h.Log("lua: loading scripts in:", path)
	d, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	defer d.Close()
	for {
		names, err := d.Readdirnames(100)
		if err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}
		for _, name := range names {
			if !strings.HasSuffix(name, ".lua") {
				continue
			}
			p.h.Log("lua: loading script:", name)
			s, err := p.loadScript(filepath.Join(path, name))
			if err != nil {
				return fmt.Errorf("lua: %v", err)
			}
			pname := s.getString("script", "name")
			vers := s.getString("script", "version")
			if pname != "" || vers != "" {
				p.h.Logf("lua: loaded plugin: %q (v%s)\n",
					s.getString("script", "name"),
					s.getString("script", "version"),
				)
			}
		}
	}
}

func (p *plugin) loadScript(path string) (*Script, error) {
	state := lua.NewState()
	s := &Script{
		p: p, h: p.h, file: path, s: state,
	}
	s.setupGlobals()
	if err := s.ExecFile(path); err != nil {
		return nil, err
	}
	p.scripts[path] = s
	return s, nil
}

func (p *plugin) Close() error {
	for _, s := range p.scripts {
		s.mu.Lock()
		s.s = nil
		s.p = nil
		s.mu.Unlock()
	}
	p.scripts = nil
	return nil
}

type M = map[string]interface{}

type UserData struct {
	Ptr interface{}
}

type Script struct {
	h    *hub.Hub
	p    *plugin
	mu   sync.Mutex
	file string
	s    *lua.State
	apis []Instance
}

func (s *Script) Hub() *hub.Hub {
	return s.h
}

func (s *Script) State() *lua.State {
	return s.s
}

func (s *Script) Name() string {
	return s.file
}

func (s *Script) setString(k, v string) {
	s.s.PushString(v)
	s.s.SetGlobal(k)
}

func (s *Script) getString(path ...string) string {
	s.getPath(path...)
	return s.popString()
}

func (s *Script) getPath(path ...string) {
	s.s.Global(path[0])
	for _, k := range path[1:] {
		if s.s.IsNil(-1) {
			s.s.PushNil()
			return
		}
		s.s.Field(-1, k)
	}
}

func (s *Script) popString() string {
	str, _ := s.s.ToString(-1)
	s.s.Pop(1)
	return str
}

func (s *Script) popBool() bool {
	return s.s.ToBoolean(-1)
}

func (s *Script) popDur() time.Duration {
	str := s.popString()
	d, _ := time.ParseDuration(str)
	return d
}

func (s *Script) pushSlice(a []interface{}) {
	s.s.CreateTable(len(a), 0)
	for i, v := range a {
		s.s.PushInteger(i + 1)
		s.Push(v)
		s.s.RawSet(-3)
	}
}

func (s *Script) pushStringMap(m map[string]string) {
	s.s.CreateTable(0, len(m))
	for k, v := range m {
		s.s.PushString(k)
		s.s.PushString(v)
		s.s.RawSet(-3)
	}
}

func (s *Script) pushRawFuncMap(m map[string]lua.Function) {
	s.s.CreateTable(0, len(m))
	for k, v := range m {
		s.s.PushString(k)
		s.s.PushGoFunction(v)
		s.s.RawSet(-3)
	}
}

func (s *Script) pushMap(m map[string]interface{}) {
	s.s.CreateTable(0, len(m))
	gi := s.s.Top()
	for k, v := range m {
		s.s.PushString(k)
		s.Push(v)
		s.s.RawSet(-3)
	}
	if s.s.Top() != gi {
		panic("invalid stack")
	}
}

func (s *Script) Push(v interface{}) {
	switch v := v.(type) {
	case UserData:
		s.s.PushUserData(v.Ptr)
		if s.s.TypeOf(-1) != lua.TypeUserData {
			panic("invalid type: " + lua.TypeNameOf(s.s, -1))
		}
	case lua.Function:
		s.s.PushGoFunction(v)
	case map[string]interface{}:
		s.pushMap(v)
	case map[string]lua.Function:
		s.pushRawFuncMap(v)
	case map[string]string:
		s.pushStringMap(v)
	case []interface{}:
		s.pushSlice(v)
	case string:
		s.s.PushString(v)
	case int:
		s.s.PushInteger(v)
	case int64:
		s.s.PushInteger(int(v))
	case uint64:
		s.s.PushNumber(float64(v))
	case float64:
		s.s.PushNumber(v)
	case bool:
		s.s.PushBoolean(v)
	case nil:
		s.s.PushNil()
	case hub.Peer:
		s.pushPeer(v)
	default:
		if k := reflect.TypeOf(v).Kind(); k == reflect.Ptr {
			s.Push(UserData{Ptr: v})
			return
		}
		// TODO: reflect
		data, _ := json.Marshal(v)
		s.h.Logf("TODO: lua.pushJSON(%T -> %q)", v, string(data))
		var m map[string]interface{}
		_ = json.Unmarshal(data, &m)
		s.pushMap(m)
	}
}

func (s *Script) Set(k string, o interface{}) {
	s.Push(o)
	s.s.SetGlobal(k)
}

func (s *Script) SetStringMap(k string, m map[string]string) {
	s.pushStringMap(m)
	s.s.SetGlobal(k)
}

func (s *Script) SetRawFuncMap(k string, m map[string]lua.Function) {
	s.pushRawFuncMap(m)
	s.s.SetGlobal(k)
}

func (s *Script) pushPeer(p hub.Peer) {
	if p == nil {
		s.Push(nil)
		return
	}
	u := p.UserInfo()
	s.pushMap(M{
		"name":  u.Name,
		"email": u.Email,
		"share": M{
			"bytes": u.Share,
		},
		"soft": M{
			"name": u.App.Name,
			"vers": u.App.Version,
		},
		"hubs": M{
			"normal":     u.HubsNormal,
			"registered": u.HubsRegistered,
			"operator":   u.HubsOperator,
		},
		"slots": M{
			"total": u.Slots,
		},
		"flags": M{
			"secure": u.TLS,
		},
		"address": p.RemoteAddr().String(),
		"sendGlobal": lua.Function(func(_ *lua.State) int {
			text, _ := s.s.ToString(1)
			s.s.Pop(1)
			_ = p.HubChatMsg(hub.Message{Text: text})
			return 0
		}),
	})
}

type Func struct {
	s   *Script
	f   interface{}
	ret int
}

func (f *Func) CallRet(ret func(st *lua.State), args ...interface{}) {
	f.s.luaCallRet(f.f, f.ret, ret, args...)
}

func (f *Func) Call(args ...interface{}) {
	if f.ret != 0 {
		panic("use CallRet to handle returns")
	}
	f.s.luaCall(f.f, f.ret, args...)
}

func (s *Script) ToFuncOn(st *lua.State, index int, ret int) *Func {
	f := st.ToValue(index)
	return &Func{s: s, f: f, ret: ret}
}

func (s *Script) ToFunc(index int, ret int) *Func {
	return s.ToFuncOn(s.s, index, ret)
}

func (s *Script) luaCallRet(fnc interface{}, ret int, post func(st *lua.State), args ...interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.s.PushLightUserData(fnc)
	for _, arg := range args {
		s.Push(arg)
	}
	s.s.Call(len(args), ret)
	if post != nil {
		post(s.s)
	}
}

func (s *Script) luaCall(fnc interface{}, ret int, args ...interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.s.PushLightUserData(fnc)
	for _, arg := range args {
		s.Push(arg)
	}
	s.s.Call(len(args), ret)
}

func (s *Script) setupGlobals() {
	lua.OpenLibraries(s.s)
	st := s.h.Stats()
	s.SetStringMap("versions", map[string]string{
		"hub":        st.Soft.Version,
		"hub_api":    apiVersion,
		"lua_plugin": s.p.Version().Vers3(),
	})
	s.SetRawFuncMap("hub", map[string]lua.Function{
		"info": func(_ *lua.State) int {
			st := s.h.Stats()
			s.Push(st)
			return 1
		},
		"userByName": func(_ *lua.State) int {
			name, _ := s.s.ToString(1)
			s.s.Pop(1)
			p := s.h.PeerByName(name)
			s.pushPeer(p)
			return 1
		},
		"users": func(_ *lua.State) int {
			var out []interface{}
			for _, p := range s.h.Peers() {
				out = append(out, p)
			}
			s.Push(out)
			return 1
		},
		"sendGlobal": func(_ *lua.State) int {
			text, _ := s.s.ToString(1)
			s.s.Pop(1)
			s.h.SendGlobalChat(text)
			return 0
		},
		"onTimer": func(_ *lua.State) int {
			str, _ := s.s.ToString(1)
			fnc := s.ToFunc(2, 0)
			s.s.Pop(2)

			dt, _ := time.ParseDuration(str)
			if dt <= 0 {
				return 0
			}
			ticker := time.NewTicker(dt)
			go func() {
				defer ticker.Stop()
				for range ticker.C {
					fnc.Call()
				}
			}()
			return 0
		},
		"onConnected": func(_ *lua.State) int {
			fnc := s.ToFunc(1, 1)
			s.s.Pop(1)
			s.h.OnConnected(func(c net.Conn) bool {
				var out bool
				fnc.CallRet(func(st *lua.State) {
					out = s.popBool()
				}, c.RemoteAddr().String())
				return out
			})
			return 0
		},
		"onDisconnected": func(_ *lua.State) int {
			fnc := s.ToFunc(1, 0)
			s.s.Pop(1)
			s.h.OnDisconnected(func(c net.Conn) {
				fnc.Call(c.RemoteAddr().String())
			})
			return 0
		},
		"onJoined": func(_ *lua.State) int {
			fnc := s.ToFunc(1, 1)
			s.s.Pop(1)
			s.h.OnJoined(func(p hub.Peer) bool {
				var out bool
				fnc.CallRet(func(st *lua.State) {
					out = s.popBool()
				}, p)
				return out
			})
			return 0
		},
		"onChat": func(_ *lua.State) int {
			fnc := s.ToFunc(1, 1)
			s.s.Pop(1)
			s.h.OnGlobalChat(func(p hub.Peer, m hub.Message) bool {
				var out bool
				fnc.CallRet(func(st *lua.State) {
					out = s.popBool()
				}, M{
					"ts":   m.Time.UTC().Unix(),
					"name": m.Name,
					"text": m.Text,
					"user": p,
				})
				return out
			})
			return 0
		},
	})
	for _, a := range apis {
		if a.Compatible(s) {
			api := a.New(s)
			s.apis = append(s.apis, api)
		}
	}
}

func (s *Script) ExecFile(path string) error {
	if err := lua.DoFile(s.s, path); err != nil {
		if err == lua.SyntaxError {
			if e, ok := s.s.ToString(1); ok && e != "" {
				return errors.New(e)
			}
		}
		return err
	}
	for _, api := range s.apis {
		api.Start()
	}
	return nil
}

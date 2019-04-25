package lua

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
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
	fmt.Println("\nlua: loading scripts in:", path)
	defer fmt.Println()
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
			fmt.Println("lua: loading script:", name)
			s, err := p.loadScript(filepath.Join(path, name))
			if err != nil {
				return err
			}
			fmt.Printf("lua: loaded plugin: %q (v%s)\n",
				s.getString("script", "name"),
				s.getString("script", "version"),
			)
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

type Script struct {
	h    *hub.Hub
	p    *plugin
	mu   sync.Mutex
	file string
	s    *lua.State
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
		s.s.Field(-1, k)
	}
}

func (s *Script) popString() string {
	str, _ := s.s.ToString(-1)
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

func (s *Script) pushStringMap(m map[string]string) {
	s.s.NewTable()
	for k, v := range m {
		s.s.PushString(v)
		s.s.SetField(-2, k)
	}
}

func (s *Script) pushRawFuncMap(m map[string]lua.Function) {
	s.s.NewTable()
	for k, v := range m {
		s.s.PushGoFunction(v)
		s.s.SetField(-2, k)
	}
}

func (s *Script) push(v interface{}) {
	switch v := v.(type) {
	case lua.Function:
		s.s.PushGoFunction(v)
	case map[string]interface{}:
		s.pushMap(v)
	case map[string]lua.Function:
		s.pushRawFuncMap(v)
	case map[string]string:
		s.pushStringMap(v)
	case []interface{}:
		s.s.NewTable()
		for i, a := range v {
			s.push(a)
			s.s.SetField(-2, strconv.Itoa(i))
		}
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
		// TODO: reflect
		data, _ := json.Marshal(v)
		var m map[string]interface{}
		_ = json.Unmarshal(data, &m)
		s.pushMap(m)
	}
}

func (s *Script) pushMap(m map[string]interface{}) {
	s.s.NewTable()
	for k, v := range m {
		s.push(v)
		s.s.SetField(-2, k)
	}
}

func (s *Script) setStringMap(k string, m map[string]string) {
	s.pushStringMap(m)
	s.s.SetGlobal(k)
}

func (s *Script) setRawFuncMap(k string, m map[string]lua.Function) {
	s.pushRawFuncMap(m)
	s.s.SetGlobal(k)
}

func (s *Script) pushPeer(p hub.Peer) {
	if p == nil {
		s.push(nil)
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

func (s *Script) callLUA(fnc interface{}, ret int, args ...interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.s.PushLightUserData(fnc)
	for _, arg := range args {
		s.push(arg)
	}
	s.s.Call(len(args), ret)
}

func (s *Script) setupGlobals() {
	lua.OpenLibraries(s.s)
	st := s.h.Stats()
	s.setStringMap("versions", map[string]string{
		"hub":        st.Soft.Version,
		"hub_api":    apiVersion,
		"lua_plugin": s.p.Version().Vers3(),
	})
	s.setRawFuncMap("hub", map[string]lua.Function{
		"info": func(_ *lua.State) int {
			st := s.h.Stats()
			s.push(st)
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
			s.push(out)
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
			fnc := s.s.ToValue(2)
			s.s.Pop(2)

			dt, _ := time.ParseDuration(str)
			if dt <= 0 {
				return 0
			}
			ticker := time.NewTicker(dt)
			go func() {
				defer ticker.Stop()
				for range ticker.C {
					s.callLUA(fnc, 0)
				}
			}()
			return 0
		},
		"onConnected": func(_ *lua.State) int {
			fnc := s.s.ToValue(1)
			s.s.Pop(1)
			s.h.OnConnected(func(c net.Conn) bool {
				s.callLUA(fnc, 1, c.RemoteAddr().String())
				return s.popBool()
			})
			return 0
		},
		"onDisconnected": func(_ *lua.State) int {
			fnc := s.s.ToValue(1)
			s.s.Pop(1)
			s.h.OnDisconnected(func(c net.Conn) {
				s.callLUA(fnc, 0, c.RemoteAddr().String())
			})
			return 0
		},
		"onJoined": func(_ *lua.State) int {
			fnc := s.s.ToValue(1)
			s.s.Pop(1)
			s.h.OnJoined(func(p hub.Peer) bool {
				s.callLUA(fnc, 1, p)
				return s.popBool()
			})
			return 0
		},
		"onChat": func(_ *lua.State) int {
			fnc := s.s.ToValue(1)
			s.s.Pop(1)
			s.h.OnChat(func(p hub.Peer, m hub.Message) bool {
				s.callLUA(fnc, 1, M{
					"ts":   m.Time.UTC().Unix(),
					"name": m.Name,
					"text": m.Text,
					"user": p,
				})
				return s.popBool()
			})
			return 0
		},
	})
}

func (s *Script) ExecFile(path string) error {
	return lua.DoFile(s.s, path)
}

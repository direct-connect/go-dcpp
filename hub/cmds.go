package hub

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"
)

type Command struct {
	Menu    []string
	Name    string
	Aliases []string
	Short   string
	Long    string
	Require string
	Func    interface{}
	run     func(p Peer, args string)
	opt     cmdOptions
}

type cmdOptions struct {
	OnUser bool
}

type CommandFunc = func(p Peer, args string) error

type RawCmd string

const (
	PermRoomsJoin = "rooms.join"
	PermRoomsList = "rooms.list"

	PermBroadcast       = "hub.broadcast"
	PermConfigWrite     = "config.write"
	PermConfigRead      = "config.read"
	PermTopic           = "hub.topic"
	PermDrop            = "user.drop"
	PermDropAll         = "user.drop_all"
	PermRedirect        = "user.redirect"
	PermRedirectAll     = "user.redirect_all"
	PermRegister        = "user.register"
	PermRegisterProfile = "user.register_profile"
	PermIP              = "user.ip"
	PermBanIP           = "ban.ip"
)

func (h *Hub) initCommands() {
	h.cmds.byName = make(map[string]*Command)
	h.cmds.names = make(map[string]struct{})
	h.RegisterCommand(Command{
		Menu: []string{"Help"},
		Name: "help", Aliases: []string{"h"},
		Short: "show the list of commands or a help for a specific command",
		Func:  h.cmdHelp,
	})
	h.RegisterCommand(Command{
		Name: "history", Aliases: []string{"log"},
		Short: "replay chat log history",
		Menu:  []string{"Chat history"},
		Func:  h.cmdChatLog,
	})
	h.RegisterCommand(Command{
		Name: "reg", Aliases: []string{"register", "passwd", "regme"},
		Short: "registers a user or change a password",
		Func:  h.cmdRegister,
	})

	// Rooms
	h.RegisterCommand(Command{
		Name:    "join",
		Short:   "join a room",
		Require: PermRoomsJoin,
		Func:    h.cmdJoin,
	})
	h.RegisterCommand(Command{
		Name: "leave", Aliases: []string{"part"},
		Short:   "leave a room",
		Require: PermRoomsJoin,
		Func:    h.cmdLeave,
	})
	h.RegisterCommand(Command{
		Name:    "rooms",
		Short:   "list available rooms",
		Menu:    []string{"Chat rooms"},
		Require: PermRoomsList,
		Func:    h.cmdRooms,
	})

	// Operator commands
	h.RegisterCommand(Command{
		Name:    "set",
		Short:   "set a config value",
		Require: PermConfigWrite,
		Func:    h.cmdConfigSet,
	})
	h.RegisterCommand(Command{
		Name:    "config",
		Aliases: []string{"get"},
		Short:   "get a config value or list all config values",
		Require: PermConfigRead,
		Func:    h.cmdConfigGet,
	})
	h.RegisterCommand(Command{
		Name:    "topic",
		Short:   "sets a hub topic",
		Require: PermTopic,
		Func:    h.cmdTopic,
	})
	h.RegisterCommand(Command{
		Name: "broadcast", Aliases: []string{"hub"},
		Short:   "broadcast a chat message to all users",
		Require: PermBroadcast,
		Func:    h.cmdBroadcast,
	})
	h.RegisterCommand(Command{
		Name: "getip", Aliases: []string{"gi"},
		Short:   "returns IP of a user",
		Menu:    []string{"IP"},
		Require: PermIP,
		Func:    h.cmdUserIP,
	})
	h.RegisterCommand(Command{
		Name: "reguser", Aliases: []string{"regnewuser"},
		Short:   "registers a new user or change a password",
		Require: PermRegister,
		Func:    h.cmdRegisterUser,
	})

	// Bans
	h.RegisterCommand(Command{
		Name:    "drop",
		Short:   "drops a user from the hub",
		Menu:    []string{"Drop"},
		Require: PermDrop,
		Func:    h.cmdDrop,
	})
	h.RegisterCommand(Command{
		Name:    "drop_all",
		Short:   "drops all users from the hub",
		Menu:    []string{"Drop All"},
		Require: PermDropAll,
		Func:    h.cmdDropAll,
	})
	h.RegisterCommand(Command{
		Name:    "banuserip",
		Short:   "ban user's IP",
		Menu:    []string{"Ban IP"},
		Require: PermBanIP,
		Func:    h.cmdBanUserIP,
	})
	h.RegisterCommand(Command{
		Name:    "banip",
		Short:   "ban a specific IP",
		Require: PermBanIP,
		Func:    h.cmdBanIP,
	})
	h.RegisterCommand(Command{
		Name:    "unbanip",
		Short:   "unban a specific IP",
		Require: PermBanIP,
		Func:    h.cmdUnBanIP,
	})
	h.RegisterCommand(Command{
		Name: "listbanip", Aliases: []string{"infoban_ipban_"},
		Short:   "list all IP bans",
		Menu:    []string{"Bans", "List IPs"},
		Require: PermBanIP,
		Func:    h.cmdListBanIP,
	})

	// Redirects
	h.RegisterCommand(Command{
		Name:    "redirect",
		Short:   "redirect user",
		Menu:    []string{"Redirect"},
		Require: PermRedirect,
		Func:    h.cmdRedirectUser,
	})
	h.RegisterCommand(Command{
		Name:    "redirect_all",
		Short:   "redirect all users",
		Menu:    []string{"Redirect All"},
		Require: PermRedirectAll,
		Func:    h.cmdRedirectAll,
	})

	// Low-level commands
	h.RegisterCommand(Command{
		Name:    "sample",
		Short:   "samples N commands received by the hub",
		Require: PermOwner,
		Func:    h.cmdSample,
	})
}

func (h *Hub) cmdHelp(p Peer, args string) error {
	u := p.User()
	if args != "" {
		name := args
		cmd := h.cmds.byName[name]
		if cmd == nil || !u.HasPerm(cmd.Require) {
			return errors.New("unsupported command: " + name)
		}
		aliases := ""
		if len(cmd.Aliases) != 0 {
			aliases = " (" + strings.Join(cmd.Aliases, ", ") + ")"
		}
		h.cmdOutputf(p, "%s%s - %s\n\n%s",
			cmd.Name, aliases, cmd.Short, cmd.Long,
		)
		return nil
	}
	names := make([]string, 0, len(h.cmds.names))
	for name := range h.cmds.names {
		c := h.cmds.byName[name]
		if !u.HasPerm(c.Require) {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	buf := bytes.NewBuffer(nil)
	buf.WriteString("available commands:\n\n")
	for _, name := range names {
		c := h.cmds.byName[name]
		buf.WriteString("- " + name)
		if len(c.Aliases) != 0 {
			buf.WriteString("  (")
			buf.WriteString(strings.Join(c.Aliases, ", "))
			buf.WriteString(")")
		}
		buf.WriteString("\n")
	}
	h.cmdOutput(p, buf.String())
	return nil
}

func (h *Hub) cmdChatLog(p Peer, args string) error {
	if h.conf.ChatLog == 0 {
		h.cmdOutput(p, "chat log is disabled on this hub")
		return nil
	}
	n := 0
	if args != "" {
		v, err := strconv.Atoi(args)
		if err != nil {
			return err
		}
		n = v
	}
	h.cmdOutputM(p, Message{Me: true, Text: "is replaying last messages"})
	h.globalChat.ReplayChat(p, n)
	return nil
}

func (h *Hub) cmdRegister(p Peer, args string) error {
	if c := p.ConnInfo(); c != nil && !c.Secure {
		return errConnInsecure
	}
	pass := args
	if err := h.validatePass(pass); err != nil {
		return err
	}
	name := p.Name()
	ok, err := h.IsRegistered(name)
	if ok {
		err = h.UpdateUser(name, func(u *UserRecord) (bool, error) {
			if pass == u.Pass {
				return false, nil
			}
			u.Pass = pass
			return true, nil
		})
		if err != nil {
			return err
		}
		h.cmdOutputf(p, "password changed")
		return nil
	}
	err = h.RegisterUser(name, args)
	if err != nil {
		return err
	}
	h.cmdOutputf(p, "user %s registered, please reconnect", name)
	return nil
}

func (h *Hub) cmdRegisterUser(p Peer, args string) error {
	if c := p.ConnInfo(); c != nil && !c.Secure {
		return errConnInsecure
	}
	name, args, err := cmdParseString(args)
	if err != nil {
		return err
	}
	if err = h.validateUserName(name); err != nil {
		return err
	}
	if ok, err := h.IsRegistered(name); err != nil {
		return err
	} else if ok {
		return errNickTaken
	}
	pass, args, err := cmdParseString(args)
	if err != nil {
		return err
	}
	if err = h.validatePass(pass); err != nil {
		return err
	}
	var prof string
	if args != "" {
		pu := p.User()
		if !pu.HasPerm(PermRegisterProfile) {
			return errors.New("you are not allowed to set user profiles during the registration")
		}
		prof, _, err = cmdParseString(args)
		if err != nil {
			return err
		}
		if prof == ProfileNameRoot && !pu.IsOwner() {
			return errors.New("only owners can register new owners")
		}
		if pr := h.Profile(prof); pr == nil {
			return errors.New("there is no such profile")
		}
	}
	err = h.RegisterUser(name, pass)
	if err != nil {
		return err
	}
	if prof == "" {
		h.cmdOutputf(p, "user %s registered", name)
		return nil
	}
	err = h.UpdateUser(name, func(u *UserRecord) (bool, error) {
		u.Profile = prof
		return true, nil
	})
	if err != nil {
		return err
	}
	h.cmdOutputf(p, "user %s with profile %s registered", name, prof)
	return nil
}

func (h *Hub) cmdJoin(p Peer, args string) error {
	name := args
	if !strings.HasPrefix(name, "#") {
		return errors.New("room name should start with '#'")
	}
	r := h.Room(name)
	if r == nil {
		var err error
		r, err = h.NewRoom(name)
		if err != nil {
			return err
		}
	}
	r.Join(p)
	return nil
}

func (h *Hub) cmdLeave(p Peer, args string) error {
	name := args
	if !strings.HasPrefix(name, "#") {
		return errors.New("room name should start with '#'")
	}
	r := h.Room(name)
	if r == nil {
		return nil
	}
	r.Leave(p)
	return nil
}

func (h *Hub) cmdRooms(p Peer, args string) error {
	list := h.Rooms()
	buf := bytes.NewBuffer(nil)
	buf.WriteString("available chat rooms:\n")
	for _, r := range list {
		buf.WriteString(r.Name() + "\n")
	}
	return nil
}

func (h *Hub) cmdConfigEcho(p Peer, key string, val interface{}) {
	buf := bytes.NewBuffer(nil)
	buf.WriteString(key)
	buf.WriteString(" = ")
	if s, ok := val.(string); ok {
		buf.WriteString(strconv.Quote(s))
	} else {
		fmt.Fprint(buf, val)
	}
	h.cmdOutputM(p, Message{Text: buf.String(), Me: true})
}

func (h *Hub) cmdTopic(p Peer, topic string) error {
	h.SetConfigString(ConfigHubTopic, topic)
	h.cmdConfigEcho(p, ConfigHubTopic, topic)
	return nil
}

func (h *Hub) cmdBroadcast(p Peer, args string) error {
	h.SendGlobalChat(args)
	return nil
}

func (h *Hub) cmdConfigSet(p Peer, key, val string) error {
	pv, _ := h.GetConfig(key)
	switch pv.(type) {
	case string:
		h.SetConfigString(key, val)
		h.cmdConfigEcho(p, key, val)
	case int64:
		v, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			return err
		}
		h.SetConfigInt(key, v)
		h.cmdConfigEcho(p, key, v)
	case uint64:
		v, err := strconv.ParseUint(val, 10, 64)
		if err != nil {
			return err
		}
		h.SetConfigUint(key, v)
		h.cmdConfigEcho(p, key, v)
	case bool:
		v, err := strconv.ParseBool(val)
		if err != nil {
			return err
		}
		h.SetConfigBool(key, v)
		h.cmdConfigEcho(p, key, v)
	case float64:
		v, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return err
		}
		h.SetConfigFloat(key, v)
		h.cmdConfigEcho(p, key, v)
	default:
		h.SetConfig(key, val)
		h.cmdConfigEcho(p, key, val)
	}
	return nil
}

func (h *Hub) cmdConfigGet(p Peer, args RawCmd) error {
	if args == "" {
		buf := bytes.NewBuffer(nil)
		buf.WriteString("config:\n")
		for _, k := range h.ConfigKeys() {
			buf.WriteString(k)
			buf.WriteString(" = ")
			v, _ := h.GetConfig(k)
			if s, ok := v.(string); ok {
				buf.WriteString(strconv.Quote(s))
			} else {
				fmt.Fprint(buf, v)
			}
			buf.WriteByte('\n')
		}
		h.cmdOutput(p, buf.String())
		return nil
	}
	k, rest, err := cmdParseString(string(args))
	if err != nil {
		return err
	} else if rest != "" {
		return errors.New("expected zero or one argument")
	}
	v, _ := h.GetConfig(k)
	h.cmdConfigEcho(p, k, v)

	return nil
}

func (h *Hub) cmdUserIP(p, p2 Peer) error {
	addr := addrString(p2.RemoteAddr())
	h.cmdOutputMu(p, p2, Message{Me: true, Text: "- " + addr})
	return nil
}

func (h *Hub) cmdDrop(p, p2 Peer) error {
	if _, ok := p2.(*botPeer); ok {
		return errors.New("refusing to kick a bot")
	}
	_ = p2.Close()
	h.cmdOutput(p, "user dropped")
	return nil
}

func (h *Hub) cmdDropAll(p Peer) error {
	n := 0
	for _, p2 := range h.Peers() {
		if p == p2 {
			continue
		} else if _, ok := p2.(*botPeer); ok {
			continue
		}
		_ = p2.Close()
		n++
	}
	h.cmdOutput(p, fmt.Sprintf("dropped %d users", n))
	return nil
}

func (h *Hub) cmdBanIPa(p Peer, ip net.IP) error {
	if ip == nil {
		return errors.New("invalid IP format")
	} else if h.IsHardBlockedIP(ip) {
		h.cmdOutput(p, "ip already blocked")
		return nil
	}
	h.HardBlockIP(ip)
	h.cmdOutput(p, "ip blocked")
	return nil
}

func (h *Hub) cmdBanUserIP(p, p2 Peer) error {
	if _, ok := p2.(*botPeer); ok {
		return errors.New("refusing to ban a bot")
	}
	addr, ok := p2.RemoteAddr().(*net.TCPAddr)
	if !ok {
		return errors.New("user is not using IP")
	}
	if err := h.cmdBanIPa(p, addr.IP); err != nil {
		return err
	}
	_ = p2.Close()
	return nil
}

func (h *Hub) cmdBanIP(p Peer, args string) error {
	ip := net.ParseIP(args)
	if ip.IsLoopback() {
		return errors.New("cannot ban loopback address")
	}
	return h.cmdBanIPa(p, ip)
}

func (h *Hub) cmdUnBanIP(p Peer, args string) error {
	ip := net.ParseIP(args)
	if ip == nil {
		return errors.New("invalid IP format")
	} else if !h.IsHardBlockedIP(ip) {
		h.cmdOutput(p, "ip is not blocked")
		return nil
	}
	h.HardUnBlockIP(ip)
	h.cmdOutput(p, "ip unblocked")
	return nil
}

func (h *Hub) cmdListBanIP(p Peer, args string) error {
	buf := bytes.NewBuffer(nil)
	buf.WriteString("blocked IPs:\n")
	h.EachHardBlockedIP(func(ip net.IP) bool {
		buf.WriteString(ip.String() + "\n")
		return true
	})
	h.cmdOutput(p, buf.String())
	return nil
}

func (h *Hub) cmdRedirectUser(p, p2 Peer, args string) error {
	if _, ok := p2.(*botPeer); ok {
		return errors.New("refusing to redirect a bot")
	}
	addr := args
	_, err := url.Parse(addr)
	if err != nil {
		return fmt.Errorf("an argument must be a valid URL: %q", addr)
	}
	if err := p2.Redirect(addr); err != nil {
		return err
	}
	_ = p2.Close()
	h.cmdOutput(p, "user redirected")
	return nil
}

func (h *Hub) cmdRedirectAll(p Peer, args string) error {
	addr := args
	_, err := url.Parse(addr)
	if err != nil {
		return fmt.Errorf("an argument must be a valid URL: %q", addr)
	}
	n := 0
	for _, p2 := range h.Peers() {
		if p == p2 {
			continue
		} else if _, ok := p2.(*botPeer); ok {
			continue
		}
		_ = p2.Redirect(addr)
		_ = p2.Close()
		n++
	}
	h.cmdOutput(p, fmt.Sprintf("redirected %d users", n))
	return nil
}

func (h *Hub) cmdSample(p Peer, args string) error {
	num := args
	pattern := ""
	if i := strings.IndexByte(args, ' '); i >= 0 {
		num, pattern = args[:i], args[i+1:]
	}
	var n uint = 10
	if num != "" {
		v, err := strconv.ParseUint(num, 10, 16)
		if err != nil {
			return err
		} else if v == 0 {
			h.sampler.stop()
			h.cmdOutputf(p, "sampling stopped")
			return nil
		}
		n = uint(v)
	}
	var cnt uint32
	if pattern == "" {
		h.cmdOutputf(p, "sampling %d commands", n)
		h.sampler.start(n, func(data []byte) bool {
			if atomic.AddUint32(&cnt, 1) > uint32(n) {
				return false
			}
			h.cmdOutput(p, strconv.Quote(string(data)))
			return true
		})
		return nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return err
	}
	h.cmdOutputf(p, "sampling %d commands (regexp)", n)
	h.sampler.start(n, func(data []byte) bool {
		if !re.Match(data) {
			return false
		} else if atomic.AddUint32(&cnt, 1) > uint32(n) {
			return false
		}
		h.cmdOutput(p, strconv.Quote(string(data)))
		return true
	})
	return nil
}

func (h *Hub) cmdOutputM(peer Peer, m Message) {
	_ = peer.HubChatMsg(m)
}

func (h *Hub) cmdOutputMu(peer, from Peer, m Message) {
	if m.Name == "" {
		m.Name = from.Name()
	}
	_ = peer.ChatMsg(nil, from, m)
}

func (h *Hub) cmdOutput(peer Peer, out string) {
	h.cmdOutputM(peer, Message{Text: out})
}

func (h *Hub) cmdOutputf(peer Peer, format string, args ...interface{}) {
	h.cmdOutput(peer, fmt.Sprintf(format, args...))
}

func (h *Hub) cmdOutputJSON(peer Peer, out interface{}) {
	data, _ := json.MarshalIndent(out, "", "  ")
	h.cmdOutput(peer, string(data))
}

var (
	reflError  = reflect.TypeOf((*error)(nil)).Elem()
	reflPeer   = reflect.TypeOf((*Peer)(nil)).Elem()
	reflDur    = reflect.TypeOf(time.Duration(0))
	reflRawCmd = reflect.TypeOf(RawCmd(""))
	reflString = reflect.TypeOf("")
	reflInt    = reflect.TypeOf(int(0))
	reflUint   = reflect.TypeOf(uint(0))
)

func cmdParseString(text string) (string, string, error) {
	if len(text) == 0 {
		return "", text, errCmdInvalidArg
	}
	if text[0] != '"' && text[0] != '`' {
		// unquoted string
		i := strings.IndexByte(text, ' ')
		if i < 0 {
			return text, "", nil
		}
		return text[:i], text[i+1:], nil
	}
	// quoted string
	v, rest, err := cmdUnquoteSplit(text)
	if err != nil {
		return "", "", errCmdInvalidArg
	}
	if rest == "" {
		return v, "", nil
	} else if rest[0] != ' ' {
		return "", "", errCmdInvalidArg
	}
	return v, rest[1:], nil
}

func cmdParseInt(text string) (int, string, error) {
	s, rest, err := cmdParseString(text)
	if err != nil {
		return 0, "", err
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, "", err
	}
	return int(v), rest, nil
}

func cmdParseUint(text string) (uint, string, error) {
	s, rest, err := cmdParseString(text)
	if err != nil {
		return 0, "", err
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, "", err
	}
	return uint(v), rest, nil
}

func cmdParseDur(text string) (time.Duration, string, error) {
	s, rest, err := cmdParseString(text)
	if err != nil {
		return 0, "", err
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return 0, "", err
	}
	return v, rest, nil
}

func (h *Hub) cmdParsePeer(text string) (Peer, string, error) {
	if len(text) == 0 {
		return nil, "", errCmdInvalidArg
	}
	v, rest, err := cmdParseString(text)
	if err != nil {
		return nil, "", err
	}
	if len(v) == 4 {
		// maybe SID
		var sid SID
		if err := sid.UnmarshalADC([]byte(v)); err == nil {
			if peer := h.peerBySID(sid); peer != nil {
				return peer, rest, nil
			}
		}
	}
	peer := h.PeerByName(v)
	if peer == nil {
		return nil, "", errors.New("no such user: " + v)
	}
	return peer, rest, nil
}

func (h *Hub) toCommandFunc(o interface{}, opt *cmdOptions) CommandFunc {
	if fnc, ok := o.(CommandFunc); ok {
		return fnc
	}
	fncv := reflect.ValueOf(o)
	rt := fncv.Type()
	if rt.Kind() != reflect.Func {
		panic("command handler should be a function")
	}
	if rt.NumOut() != 1 {
		panic("expected exactly one output")
	} else if rt.Out(0) != reflError {
		panic("return type should be an error")
	}
	selfInd := -1
	argc := rt.NumIn()
	hasRaw := false
	argt := make([]reflect.Type, 0, argc)
	for i := 0; i < argc; i++ {
		t := rt.In(i)
		if selfInd < 0 && t == reflPeer {
			selfInd = i
		} else if t == reflRawCmd {
			if i != argc-1 {
				panic("raw command can only be the last argument")
			}
			hasRaw = true
		} else {
			switch t {
			case reflPeer:
				if argc == 1 || (argc == 2 && selfInd >= 0) {
					opt.OnUser = true
				}
			case reflDur:
			case reflInt, reflUint, reflString:
			default:
				panic(fmt.Errorf("unsupported type: %v", t))
			}
		}
		argt = append(argt, t)
	}
	argOffs := 0
	if selfInd >= 0 {
		argOffs++
	}
	return func(p Peer, args string) error {
		in := make([]reflect.Value, 0, len(argt))
		for i, t := range argt {
			if i == selfInd {
				in = append(in, reflect.ValueOf(p))
				continue
			} else if hasRaw && i == argc-1 {
				in = append(in, reflect.ValueOf(RawCmd(args)))
				continue
			}
			var (
				v   interface{}
				err error
			)
			switch t {
			case reflPeer:
				v, args, err = h.cmdParsePeer(args)
			case reflString:
				v, args, err = cmdParseString(args)
			case reflInt:
				v, args, err = cmdParseInt(args)
			case reflUint:
				v, args, err = cmdParseUint(args)
			case reflDur:
				v, args, err = cmdParseDur(args)
			default:
				return fmt.Errorf("unsupported type: %v", t)
			}
			if err != nil {
				i -= argOffs
				return fmt.Errorf("arg %d: %v", i+1, err)
			}
			in = append(in, reflect.ValueOf(v))
		}
		ev := fncv.Call(in)[0]
		if ev.IsNil() {
			return nil
		}
		return ev.Interface().(error)
	}
}

func (h *Hub) RegisterCommand(cmd Command) {
	fnc := h.toCommandFunc(cmd.Func, &cmd.opt)
	cmd.run = func(p Peer, args string) {
		err := fnc(p, args)
		if err != nil {
			h.cmdOutput(p, "error: "+err.Error())
		}
	}
	h.cmds.names[cmd.Name] = struct{}{}
	h.cmds.byName[cmd.Name] = &cmd
	for _, name := range cmd.Aliases {
		h.cmds.byName[name] = &cmd
	}
}

func (h *Hub) isCommand(peer Peer, text string) bool {
	if text == "" {
		return true // pretend that this is a command (ping)
	} else if text == "/fav" {
		return true // special case, client-side command
	} else if len(text) == 1 {
		return false // "+" or "!" are valid chat messages
	}
	switch text[0] {
	case '/':
		if text == "/me" || strings.HasPrefix(text[1:], "me ") {
			return false
		}
	case '!', '+':
	default:
		return false
	}
	cmd := text[1:]
	i := strings.IndexByte(cmd, ' ')
	args := ""
	if i > 0 {
		args = cmd[i+1:]
		cmd = cmd[:i]
	}
	if len(cmd) == 0 {
		return false // allow any combinations of "+ ..." or "! ..."
	}
	if r, _ := utf8.DecodeRuneInString(cmd); !unicode.IsLetter(r) {
		return false // "!!..." or any other weird things
	}
	h.command(peer, cmd, args)
	return true
}

func (h *Hub) peerHasPerm(peer Peer, perm string) bool {
	if perm == "" {
		return true
	}
	return peer.User().HasPerm(perm)
}

func (h *Hub) command(peer Peer, cmd string, args string) {
	c, ok := h.cmds.byName[cmd]
	if !ok || !h.peerHasPerm(peer, c.Require) {
		h.cmdOutput(peer, "unsupported command: "+cmd)
		return
	}
	c.run(peer, args)
}

func (h *Hub) ListCommands(u *User) []*Command {
	command := make([]*Command, 0, len(h.cmds.names))
	for name := range h.cmds.names {
		c := h.cmds.byName[name]
		if len(c.Menu) == 0 {
			continue
		} else if c.Require != "" && !u.IsOwner() && !u.HasPerm(c.Require) {
			continue
		}
		command = append(command, c)
	}
	sort.Slice(command, func(i, j int) bool {
		a, b := command[i], command[j]
		l := len(a.Menu)
		if len(a.Menu) > len(b.Menu) {
			l = len(b.Menu)
		}
		for n := 0; n <= l; n++ {
			if a.Menu[n] != b.Menu[n] {
				return a.Menu[n] < b.Menu[n]
			}
		}
		return len(a.Menu) <= len(b.Menu)
	})
	return command
}

package hub

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
)

type Command struct {
	Menu    []string
	Name    string
	Aliases []string
	Short   string
	Long    string
	Require string
	Func    func(p Peer, args string) error
	run     func(p Peer, args string)
}

const (
	PermRoomsJoin = "rooms.join"
	PermRoomsList = "rooms.list"

	PermBroadcast = "hub.broadcast"
	PermDrop      = "user.drop"
	PermIP        = "user.ip"
	PermBanIP     = "ban.ip"
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
		Name:  "log",
		Short: "replay chat log",
		Func:  h.cmdChatLog,
	})
	h.RegisterCommand(Command{
		Name: "reg", Aliases: []string{"register", "passwd"},
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
		Require: PermRoomsList,
		Func:    h.cmdRooms,
	})

	// Operator commands
	h.RegisterCommand(Command{
		Name: "broadcast", Aliases: []string{"hub"},
		Short:   "broadcast a chat message to all users",
		Require: PermBroadcast,
		Func:    h.cmdBroadcast,
	})
	h.RegisterCommand(Command{
		Name: "getip", Aliases: []string{"gi"},
		Short:   "returns IP of a user",
		Require: PermIP,
		Func:    h.cmdUserIP,
	})

	// Bans
	h.RegisterCommand(Command{
		Name:    "drop",
		Short:   "drops a user from the hub",
		Require: PermDrop,
		Func:    h.cmdDrop,
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
		Require: PermBanIP,
		Func:    h.cmdListBanIP,
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
	h.cmdOutput(p, "replaying last messages")
	h.globalChat.ReplayChat(p, n)
	return nil
}

func (h *Hub) cmdRegister(p Peer, args string) error {
	if c := p.ConnInfo(); c != nil && !c.Secure {
		return errConnInsecure
	}
	if len(args) < 6 {
		h.cmdOutput(p, "password should be at least 6 characters")
		return nil
	}
	name := p.Name()
	pass := args
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

func (h *Hub) cmdBroadcast(p Peer, args string) error {
	h.SendGlobalChat(args)
	return nil
}

func (h *Hub) cmdUserIP(p Peer, args string) error {
	p2 := h.PeerByName(args)
	if p2 == nil {
		return errors.New("no such user")
	}
	h.cmdOutput(p, "IP: "+addrString(p2.RemoteAddr()))
	return nil
}

func (h *Hub) cmdDrop(p Peer, args string) error {
	p2 := h.PeerByName(args)
	if p2 == nil {
		return errors.New("no such user")
	}
	_ = p2.Close()
	h.cmdOutput(p, "user dropped")
	return nil
}

func (h *Hub) cmdBanIP(p Peer, args string) error {
	ip := net.ParseIP(args)
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

func (h *Hub) cmdOutput(peer Peer, out string) {
	_ = peer.HubChatMsg(out)
}

func (h *Hub) cmdOutputf(peer Peer, format string, args ...interface{}) {
	h.cmdOutput(peer, fmt.Sprintf(format, args...))
}

func (h *Hub) cmdOutputJSON(peer Peer, out interface{}) {
	data, _ := json.MarshalIndent(out, "", "  ")
	h.cmdOutput(peer, string(data))
}

func (h *Hub) RegisterCommand(cmd Command) {
	cmd.run = func(p Peer, args string) {
		err := cmd.Func(p, args)
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
		return true // pretend that this is a command
	} else if text == "/fav" {
		return true // special case
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
	sub := strings.SplitN(text, " ", 2)
	cmd := sub[0][1:]
	args := ""
	if len(sub) > 1 {
		args = sub[1]
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

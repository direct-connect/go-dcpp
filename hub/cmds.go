package hub

import (
	"bytes"
	"errors"
	"sort"
	"strconv"
	"strings"
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
	h.RegisterCommand(Command{
		Name:  "join",
		Short: "join a room",
		Func:  h.cmdJoin,
	})
	h.RegisterCommand(Command{
		Name: "leave", Aliases: []string{"part"},
		Short: "leave a room",
		Func:  h.cmdLeave,
	})
}

func (h *Hub) cmdHelp(p Peer, args string) error {
	if args != "" {
		name := args
		cmd := h.cmds.byName[name]
		if cmd == nil {
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
	buf := bytes.NewBuffer(nil)
	buf.WriteString("available commands:\n\n")
	names := make([]string, 0, len(h.cmds.names))
	for name := range h.cmds.names {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		buf.WriteString("- " + name)
		if c := h.cmds.byName[name]; len(c.Aliases) != 0 {
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
	if len(args) < 6 {
		h.cmdOutput(p, "password should be at least 6 characters")
		return nil
	}
	name := p.Name()
	err := h.RegisterUser(name, args)
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

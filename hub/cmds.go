package hub

import (
	"bytes"
	"errors"
	"net"
	"sort"
	"strconv"
	"strings"
)

func (h *Hub) initCommands() {
	h.cmds.byName = make(map[string]*Command)
	h.registerCommand(Command{
		Name: "help", Aliases: []string{"h"},
		Short: "show the list of commands or a help for a specific command",
		Func:  cmdHelp,
	})
	h.registerCommand(Command{
		Name:  "stats",
		Short: "show hub stats",
		Func:  cmdStats,
	})
	h.registerCommand(Command{
		Name:  "log",
		Short: "replay chat log",
		Func:  cmdChatLog,
	})
	h.registerCommand(Command{
		Name: "reg", Aliases: []string{"register", "passwd"},
		Short: "registers a user or change a password",
		Func:  cmdRegister,
	})
	h.registerCommand(Command{
		Name: "myip", Aliases: []string{"ip"},
		Short: "shows your current ip",
		Func:  cmdIP,
	})
}

func cmdHelp(h *Hub, p Peer, args string) error {
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
	names := make([]string, 0, len(h.cmds.byName))
	for name := range h.cmds.byName {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		buf.WriteString("- " + name + "\n")
	}
	h.cmdOutput(p, buf.String())
	return nil
}

func cmdStats(h *Hub, p Peer, args string) error {
	st := h.Stats()
	h.cmdOutputJSON(p, st)
	return nil
}

func cmdChatLog(h *Hub, p Peer, args string) error {
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
	h.replayChat(p, n)
	return nil
}

func cmdIP(h *Hub, p Peer, args string) error {
	host, _, _ := net.SplitHostPort(p.RemoteAddr().String())
	h.cmdOutput(p, "IP: "+host)
	return nil
}

func cmdRegister(h *Hub, p Peer, args string) error {
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

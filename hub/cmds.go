package hub

import (
	"bytes"
	"errors"
	"sort"
	"strconv"
	"strings"
)

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

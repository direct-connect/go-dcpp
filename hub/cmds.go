package hub

import (
	"bytes"
	"errors"
	"sort"
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

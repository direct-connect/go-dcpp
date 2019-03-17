package hubstats

import (
	"encoding/json"

	"github.com/direct-connect/go-dcpp/hub"
)

func init() {
	hub.RegisterPlugin(&hubStats{})
}

type hubStats struct {
	h *hub.Hub
}

func (*hubStats) Name() string {
	return "HubStats"
}

func (*hubStats) Version() hub.Version {
	return hub.Version{Major: 0, Minor: 2}
}

func (p *hubStats) Init(h *hub.Hub) error {
	p.h = h
	h.RegisterCommand(hub.Command{
		Menu:  []string{"Stats"},
		Name:  "stats",
		Short: "show hub stats",
		Func:  p.cmdStats,
	})
	return nil
}

func (p *hubStats) cmdStats(peer hub.Peer, args string) error {
	st := p.h.Stats()
	data, _ := json.MarshalIndent(st, "", "  ")
	_ = peer.HubChatMsg(string(data))
	return nil
}

func (p *hubStats) Close() error {
	return nil
}

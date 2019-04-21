package hubstats

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"sort"

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
	return hub.Version{Major: 0, Minor: 4}
}

func (p *hubStats) Init(h *hub.Hub, path string) error {
	p.h = h
	h.RegisterCommand(hub.Command{
		Menu:    []string{"Stats"},
		Name:    "stats",
		Aliases: []string{"hubinfo"},
		Short:   "show hub stats",
		Func:    p.cmdStats,
	})
	h.RegisterCommand(hub.Command{
		Name:  "tlsinfo",
		Short: "show info about TLS connections",
		Func:  p.cmdTLSStats,
	})
	return nil
}

func (p *hubStats) cmdStats(peer hub.Peer, args string) error {
	st := p.h.Stats()
	data, _ := json.MarshalIndent(st, "", "  ")
	_ = peer.HubChatMsg(string(data))
	return nil
}

func (p *hubStats) cmdTLSStats(peer hub.Peer, args string) error {
	var s struct {
		Total uint
		TLS   uint
		TLS10 uint
		TLS11 uint
		TLS12 uint
		TLS13 uint
		ALPN  uint
	}
	alpn := make(map[string]uint)
	for _, p2 := range p.h.Peers() {
		s.Total++
		c := p2.ConnInfo()
		if c == nil {
			continue
		}
		if c.TLSVers != 0 {
			s.TLS++
		}
		if c.ALPN != "" {
			s.ALPN++
			alpn[c.ALPN]++
		}
		switch c.TLSVers {
		case tls.VersionTLS10:
			s.TLS10++
		case tls.VersionTLS11:
			s.TLS11++
		case tls.VersionTLS12:
			s.TLS12++
		case tls.VersionTLS13:
			s.TLS13++
		default:
			if c.TLSVers > tls.VersionTLS13 {
				s.TLS13++
			}
		}
	}
	perc := func(v, n uint) float64 {
		return 100 * float64(v) / float64(n)
	}
	buf := bytes.NewBuffer(nil)
	_, _ = fmt.Fprintf(buf, `TLS statistics:
Users:  %5d
TLS:   %5d (%.1f%%)
ALPN: %5d (%.1f%%)

TLS verions:
1.0: %5d (%.1f%%)
1.1: %5d (%.1f%%)
1.2: %5d (%.1f%%)
1.3: %5d (%.1f%%)
`,
		s.Total,
		s.TLS, perc(s.TLS, s.Total),
		s.ALPN, perc(s.ALPN, s.TLS),
		s.TLS10, perc(s.TLS10, s.TLS),
		s.TLS11, perc(s.TLS11, s.TLS),
		s.TLS12, perc(s.TLS12, s.TLS),
		s.TLS13, perc(s.TLS13, s.TLS),
	)
	if len(alpn) != 0 {
		buf.WriteString("\nALPN protocols:\n")
		names := make([]string, 0, len(alpn))
		for name := range alpn {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			v := alpn[name]
			_, _ = fmt.Fprintf(buf,
				"%s: %5d (%.1f%%)\n",
				name, v, perc(v, s.ALPN),
			)
		}
	}
	_ = peer.HubChatMsg(buf.String())
	return nil
}

func (p *hubStats) Close() error {
	return nil
}

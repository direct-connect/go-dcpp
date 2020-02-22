package updates

import (
	"context"
	"fmt"
	"time"

	"github.com/direct-connect/go-dcpp/hub"
	"github.com/direct-connect/go-dcpp/internal/safe"
	"github.com/direct-connect/go-dcpp/version"
)

const (
	day             = time.Hour * 24
	year            = day * 365
	complainToOwner = day * 30 * 2
	complainToAll   = year / 2
)

func init() {
	hub.RegisterPlugin(&hubUpdatesChecker{})
}

type hubUpdatesChecker struct {
	h *hub.Hub

	built   time.Time
	veryOld safe.Bool
	latest  safe.Time
}

func (*hubUpdatesChecker) Name() string {
	return "HubUpdatesChecker"
}

func (*hubUpdatesChecker) Version() hub.Version {
	return hub.Version{Major: 1, Minor: 0}
}

func (p *hubUpdatesChecker) Init(h *hub.Hub, path string) error {
	p.h = h
	if t, err := time.Parse(version.TimestampFormat, version.Timestamp); err == nil {
		p.built = t
	} else {
		p.built = time.Now().Add(-h.Uptime())
	}
	h.OnJoined(func(peer hub.Peer) bool {
		if !peer.User().IsOwner() {
			if p.veryOld.Get() {
				p.complainToUser(peer)
			}
			return true
		}
		go p.checkUpdates(peer)
		return true
	})
	go func() {
		ticker := time.NewTicker(day * 7)
		defer ticker.Stop()
		for range ticker.C {
			if r, _ := p.check(); r != nil {
				if !p.veryOld.Get() && time.Since(p.built) > complainToAll {
					p.veryOld.Set(true)
				}
			}
		}
	}()
	return nil
}

func (p *hubUpdatesChecker) check() (*hub.ReleaseInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	r, err := hub.CheckForUpdates(ctx, false)
	if err != nil {
		return nil, err
	} else if r == nil {
		return nil, nil
	}
	p.latest.Set(r.Time)
	return r, nil
}

func (p *hubUpdatesChecker) checkUpdates(peer hub.Peer) {
	r, err := p.check()
	if err != nil {
		_ = peer.HubChatMsg(hub.Message{
			Me:   true,
			Text: fmt.Sprintf("failed to check updates: %v", err),
		})
		return
	}
	if r == nil {
		return
	}
	desc := r.Desc
	if desc == "" {
		desc = "\n\n" + desc
	}
	if time.Since(r.Time) > complainToOwner || time.Since(p.built) > complainToOwner {
		// start complaining to owner(s)
		p.complainToOwner(peer, r)
	} else {
		// soft notification
		p.notifyAboutUpdate(peer, r)
	}
}

func (p *hubUpdatesChecker) notifyAboutUpdate(peer hub.Peer, r *hub.ReleaseInfo) {
	_ = peer.HubChatMsg(hub.Message{
		Me: true,
		Text: fmt.Sprintf(`version %s was released!%s

Please download a new version at:
%s`, r.Version, r.Desc, r.URL),
	})
}

func (p *hubUpdatesChecker) complainToOwner(peer hub.Peer, r *hub.ReleaseInfo) {
	_ = peer.HubChatMsg(hub.Message{
		Me: true,
		Text: fmt.Sprintf(`WARNING! New hub version %s was released %d days ago! You are running version %s.

%s, using old hub version is a BAD IDEA. It may have PERFORMANCE issues, SECURITY issues and even VULNERABILITIES that may get your server exploited and/or hacked.

Download a new version at:
%s`,
			r.Version, int(time.Since(r.Time).Hours())/24, version.Vers, peer.Name(), r.URL,
		),
	})
}

func (p *hubUpdatesChecker) complainToUser(peer hub.Peer) {
	_ = peer.HubChatMsg(hub.Message{
		Me: true,
		Text: fmt.Sprintf(`WARNING! Hub's software wasn't updated for at least %d days!

%s, connecting to old hubs like this one is a BAD IDEA. It may have PERFORMANCE issues, SECURITY issues and even VULNERABILITIES that may get your client exploited or compromised.

Please contact the hub owner to update the software or connect to a different hub!`,
			int(time.Since(p.built).Hours())/24, p.Name(),
		),
	})
}

func (p *hubUpdatesChecker) Close() error {
	return nil
}

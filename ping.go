package dc

import (
	"context"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	nmdcp "github.com/direct-connect/go-dc/nmdc"
	"github.com/direct-connect/go-dcpp/adc"
	"github.com/direct-connect/go-dcpp/nmdc"
)

type PingConfig = adc.PingConfig

// Ping fetches the information about the specified hub.
func Ping(ctx context.Context, addr string, conf *PingConfig) (*HubInfo, error) {
	if conf == nil {
		conf = &PingConfig{}
	}
	if conf.Name == "" {
		num := int64(time.Now().Nanosecond())
		conf.Name = "pinger_" + strconv.FormatInt(num, 16)
	}
	if conf.Hubs == 0 {
		conf.Hubs = 1 + rand.Intn(10)
	}
	if conf.Slots == 0 {
		conf.Slots = 5
	}
	if conf.ShareFiles == 0 {
		conf.ShareFiles = 100 + rand.Intn(1000)
	}
	if conf.ShareSize == 0 {
		conf.ShareSize = uint64(100+rand.Intn(200)) * 1023 * 1023 * 1023
	}

	// probe first, if protocol is not specified
	i := strings.Index(addr, "://")
	if i < 0 {
		u, err := Probe(ctx, addr)
		if err != nil {
			return nil, err
		}
		addr = u.String()
		i = strings.Index(addr, "://")
	}

	switch addr[:i] {
	case nmdcSchema, nmdcsSchema:
		hub, err := nmdc.Ping(ctx, addr, nmdc.PingConfig{
			Name: conf.Name, Share: conf.ShareSize,
			Slots: conf.Slots, Hubs: conf.Hubs,
		})
		if err == nmdc.ErrRegisteredOnly {
			// TODO: should support error code in NMDC
			err = adc.Error{Status: adc.Status{
				Sev: adc.Fatal, Code: 26,
				Msg: err.Error(),
			}}
		}
		if err != nil && hub == nil {
			return nil, err
		}
		info := &HubInfo{
			Name: hub.Name,
			Desc: hub.Desc,
			Addr: []string{addr},
			Enc:  hub.Encoding,
			Server: &Software{
				Name:    hub.Server.Name,
				Version: hub.Server.Version,
				Ext:     hub.Ext,
			},
			Users:    len(hub.Users),
			UserList: make([]HubUser, 0, len(hub.Users)),
		}
		if info.Desc == "" {
			info.Desc = hub.Topic
		}
		if hub.Addr != "" {
			if uri, err := nmdcp.NormalizeAddr(hub.Addr); err == nil && uri != addr {
				info.Addr = append(info.Addr, uri)
			}
		}
		for _, a := range hub.Failover {
			uri, err := nmdcp.NormalizeAddr(a)
			if err == nil {
				info.Addr = append(info.Addr, uri)
			}
		}

		for _, u := range hub.Users {
			info.Share += uint64(u.ShareSize)
			user := HubUser{
				Name:  string(u.Name),
				Share: u.ShareSize,
				Email: u.Email,
				Client: &Software{
					Name:    u.Client.Name,
					Version: u.Client.Version,
				},
			}
			if u.Flag&nmdcp.FlagTLS != 0 {
				user.Client.Ext = append(user.Client.Ext, "TLS")
			}
			if u.Flag&nmdcp.FlagIPv4 != 0 {
				user.Client.Ext = append(user.Client.Ext, adc.FeaTCP4.String())
			}
			if u.Flag&nmdcp.FlagIPv6 != 0 {
				user.Client.Ext = append(user.Client.Ext, adc.FeaTCP6.String())
			}
			info.UserList = append(info.UserList, user)
		}
		return info, err
	case adcSchema, adcsSchema:
		hub, err := adc.Ping(ctx, addr, *conf)
		if err != nil && hub == nil {
			return nil, err
		}
		info := &HubInfo{
			Name:    hub.Name,
			Desc:    hub.Desc,
			Addr:    []string{addr},
			Enc:     "utf-8",
			Website: hub.Website,
			Owner:   hub.Owner,
			Uptime:  uint64(hub.Uptime),
			Server: &Software{
				Name:    hub.Application,
				Version: hub.Version,
				Ext:     hub.Ext,
			},
			Users:    len(hub.Users),
			UserList: make([]HubUser, 0, len(hub.Users)),
		}
		for _, u := range hub.Users {
			u.Normalize()
			info.Files += uint64(u.ShareFiles)
			info.Share += uint64(u.ShareSize)
			user := HubUser{
				Name:  string(u.Name),
				Files: u.ShareFiles,
				Share: uint64(u.ShareSize),
				Email: u.Email,
				Client: &Software{
					Name:    u.Application,
					Version: u.Version,
				},
			}
			for _, f := range u.Features {
				user.Client.Ext = append(user.Client.Ext, f.String())
			}
			info.UserList = append(info.UserList, user)
		}
		return info, err
	default:
		return nil, fmt.Errorf("unsupported protocol: %q", addr)
	}
}

type HubInfo struct {
	Name     string    `json:"name" xml:"Name,attr"`
	Desc     string    `json:"desc,omitempty" xml:"Description,attr,omitempty"`
	Addr     []string  `json:"addr,omitempty" xml:"Address,attr,omitempty"`
	Icon     string    `json:"icon,omitempty" xml:"Icon,attr,omitempty"`
	Owner    string    `json:"owner,omitempty" xml:"Owner,attr,omitempty"`
	Website  string    `json:"website,omitempty" xml:"Website,attr,omitempty"`
	Email    string    `json:"email,omitempty" xml:"Email,attr,omitempty"`
	Enc      string    `json:"encoding,omitempty" xml:"Encoding,attr,omitempty"`
	Server   *Software `json:"soft,omitempty" xml:"Software,omitempty"`
	Uptime   uint64    `json:"uptime,omitempty" xml:"Uptime,attr,omitempty"`
	Users    int       `json:"users" xml:"Users,attr"`
	Files    uint64    `json:"files,omitempty" xml:"Files,attr,omitempty"`
	Share    uint64    `json:"share,omitempty" xml:"Shared,attr,omitempty"`
	UserList []HubUser `json:"userlist,omitempty" xml:"User,attr,omitempty"`
}

type HubUser struct {
	Name   string    `json:"name" xml:"Name,attr"`
	Client *Software `json:"soft,omitempty" xml:"Software,omitempty"`
	Files  int       `json:"files,omitempty" xml:"Files,attr,omitempty"`
	Share  uint64    `json:"share,omitempty" xml:"Shared,attr,omitempty"`
	Email  string    `json:"email,omitempty" xml:"Email,attr,omitempty"`
}

// Software version.
type Software struct {
	Name    string   `json:"name" xml:"Name,attr"`
	Version string   `json:"vers,omitempty" xml:"Version,attr,omitempty"`
	Ext     []string `json:"ext,omitempty" xml:"Ext,attr,omitempty"`
}

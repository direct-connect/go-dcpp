package adc

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/direct-connect/go-dc/adc"
	"github.com/direct-connect/go-dc/adc/types"
)

type PingHubInfo struct {
	adc.HubInfo
	Ext       []string
	KeyPrints []string
	Users     []adc.UserInfo
}

type PingConfig struct {
	Name       string
	ShareSize  uint64
	ShareFiles int
	Slots      int
	Hubs       int
}

func Ping(ctx context.Context, addr string, conf PingConfig) (*PingHubInfo, error) {
	c, err := DialContext(ctx, addr)
	if err != nil {
		return nil, err
	}
	defer c.Close()

	fea := adc.ModFeatures{
		// should always be set for ADC
		adc.FeaBASE: true,
		adc.FeaBAS0: true,
		adc.FeaTIGR: true,
		// extensions

		// TODO: some hubs will stop the handshake after sending the hub info
		//       if this extension is specified
		//adc.FeaPING: true,

		adc.FeaUCMD: true,
		adc.FeaUCM0: true,
		adc.FeaBZIP: true,
		adc.FeaSEGA: true,
		adc.FeaZLIF: true,
		adc.FeaONID: true,
		adc.FeaASCH: true,
		adc.FeaNAT0: true,
		// TODO: anything else?
	}

	err = c.WriteHubMsg(adc.Supported{
		Features: fea,
	})
	if err != nil {
		return nil, err
	}
	if err := c.Flush(); err != nil {
		return nil, err
	}

	// set deadline once
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(time.Second * 10)
	}
	if err = c.conn.SetDeadline(deadline); err != nil {
		return nil, err
	}

	var hub PingHubInfo
	hub.KeyPrints = c.GetKeyPrints()

	// first, we expect a SUP from the hub with a list of supported features
	msg, err := c.ReadInfoMsg(deadline)
	if err != nil {
		return nil, err
	}
	if _, ok := msg.(adc.ZOn); ok {
		err = c.r.EnableZlib()
		if err != nil {
			return nil, err
		}
		msg, err = c.ReadInfoMsg(deadline)
		if err != nil {
			return nil, err
		}
	}
	sup, ok := msg.(adc.Supported)
	if !ok {
		return nil, fmt.Errorf("expected SUP command, got: %#v", msg)
	}
	var ext []adc.Feature
	for features, ok := range sup.Features {
		if !ok {
			continue
		}
		ext = append(ext, features)
		hub.Ext = append(hub.Ext, features.String())
	}

	// next, we expect a SID that will assign a Session ID
	msg, err = c.ReadInfoMsg(deadline)
	if err != nil {
		return nil, err
	}
	sid, ok := msg.(adc.SIDAssign)
	if !ok {
		return nil, fmt.Errorf("expected SID command, got: %#v", msg)
	}

	pid, err := types.NewPID()
	if err != nil {
		return nil, err
	}
	user := adc.UserInfo{
		Id:         pid.Hash(),
		Pid:        &pid,
		Name:       conf.Name,
		Features:   ext,
		Slots:      conf.Slots,
		SlotsFree:  conf.Slots,
		ShareSize:  int64(conf.ShareSize),
		ShareFiles: conf.ShareFiles,
		HubsNormal: conf.Hubs,
	}

	err = c.WriteBroadcast(sid.SID, user)
	if err != nil {
		return nil, err
	}
	if err := c.Flush(); err != nil {
		return nil, err
	}

	fixVersion := func() {
		if hub.Application != "" {
			return
		}
		if strings.HasPrefix(hub.Version, "FlexHub") {
			i := strings.LastIndex(hub.Version, "Beta ")
			if i > 0 {
				i += 4
			} else {
				i = strings.LastIndex(hub.Version, " ")
			}
			if i > 0 {
				hub.Application, hub.Version = hub.Version[:i], hub.Version[i+1:]
			}
		}
	}

	// next, we expect a INF from the hub
	const (
		hubInfo = iota
		optStatus
		userList
	)
	stage := hubInfo
	for {
		pck, err := c.ReadPacketRaw(deadline)
		if err == io.EOF {
			return &hub, io.ErrUnexpectedEOF
		} else if err != nil {
			return &hub, err
		}
		switch pck := pck.(type) {
		case *adc.InfoPacket:
			typ := pck.Msg.Cmd()
			if typ == (adc.ZOn{}).Cmd() {
				err = c.r.EnableZlib()
				if err != nil {
					return &hub, err
				}
				continue
			}
			switch stage {
			case hubInfo:
				// waiting for hub info
				if typ != (adc.UserInfo{}).Cmd() {
					return &hub, fmt.Errorf("expected hub info, received: %v", typ)
				}
				if err := pck.DecodeMessageTo(&hub.HubInfo); err != nil {
					return &hub, err
				}
				fixVersion()
				stage = optStatus
			case optStatus:
				// optionally wait for status command
				if typ == (adc.ChatMessage{}).Cmd() {
					continue // ignore
				}
				if typ != (adc.Status{}).Cmd() {
					return &hub, fmt.Errorf("expected status, received: %#v", pck)
				}
				var st adc.Status
				if err := pck.DecodeMessageTo(&st); err != nil {
					return &hub, err
				} else if !st.Ok() {
					return &hub, st.Err()
				}
				stage = userList
			case userList:
				if typ != (adc.Disconnect{}).Cmd() {
					return &hub, fmt.Errorf("expected disconnect, received: %#v", pck)
				}
				var st adc.Disconnect
				if err := pck.DecodeMessageTo(&st); err != nil {
					return &hub, err
				}
				e := adc.Error{Status: adc.Status{Sev: adc.Fatal, Msg: st.Message}}
				if strings.Contains(st.Message, "registered users only") {
					e.Code = 26
				}
				if e.Msg == "" {
					e.Msg = "hub closed the connection"
				}
				return &hub, e
			default:
				return &hub, fmt.Errorf("unexpected command in stage %d: %#v", stage, pck)
			}
			continue
		case *adc.BroadcastPacket:
			switch stage {
			case optStatus:
				stage = userList
				fallthrough
			case userList:
				if pck.ID == sid.SID {
					// make sure to wipe PID, so we don't send it later occasionally
					user.Pid = nil
					if err := pck.DecodeMessageTo(&user); err != nil {
						return &hub, err
					}
					// done, should switch to NORMAL
					return &hub, nil
				}
				// other users
				var u adc.UserInfo
				if err := pck.DecodeMessageTo(&u); err != nil {
					return &hub, err
				}
				if hub.Version == "" && hub.Application == "" {
					if strings.HasPrefix(u.Version, "FlexHub") {
						hub.Version = u.Version
						hub.Application = u.Application
						fixVersion()
						continue
					}
				}
				hub.Users = append(hub.Users, u)
				// continue until we see ourselves in the list
			default:
				return &hub, fmt.Errorf("unexpected command in stage %d: %#v", stage, pck)
			}
		default:
			return &hub, fmt.Errorf("unexpected command: %#v", pck)
		}
	}
}

package adc

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/direct-connect/go-dcpp/adc/types"
)

type PingHubInfo struct {
	HubInfo
	Ext   []string
	Users []User
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

	fea := ModFeatures{
		// should always be set for ADC
		FeaBASE: true,
		FeaBAS0: true,
		FeaTIGR: true,
		// extensions

		// TODO: some hubs will stop the handshake after sending the hub info
		//       if this extension is specified
		//adc.FeaPING: true,

		FeaUCMD: true,
		FeaUCM0: true,
		FeaBZIP: true,
		FeaSEGA: true,
		FeaZLIF: true,
		extONID: true,
		extASCH: true,
		extNAT0: true,
		// TODO: anything else?
	}

	err = c.WriteHubMsg(Supported{
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

	// first, we expect a SUP from the hub with a list of supported features
	msg, err := c.ReadInfoMsg(deadline)
	if err != nil {
		return nil, err
	}
	if _, ok := msg.(ZOn); ok {
		err = c.r.EnableZlib()
		if err != nil {
			return nil, err
		}
		msg, err = c.ReadInfoMsg(deadline)
		if err != nil {
			return nil, err
		}
	}
	sup, ok := msg.(Supported)
	if !ok {
		return nil, fmt.Errorf("expected SUP command, got: %#v", msg)
	}
	var ext []Feature
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
	sid, ok := msg.(SIDAssign)
	if !ok {
		return nil, fmt.Errorf("expected SID command, got: %#v", msg)
	}

	pid := types.NewPID()
	user := User{
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
		cmd, err := c.ReadPacket(deadline)
		if err == io.EOF {
			return &hub, io.ErrUnexpectedEOF
		} else if err != nil {
			return &hub, err
		}
		switch cmd := cmd.(type) {
		case *InfoPacket:
			if cmd.Name == (ZOn{}).Cmd() {
				err = c.r.EnableZlib()
				if err != nil {
					return &hub, err
				}
				continue
			}
			switch stage {
			case hubInfo:
				// waiting for hub info
				if cmd.Name != (User{}).Cmd() {
					return &hub, fmt.Errorf("expected hub info, received: %#v", cmd.Data)
				}
				if err := Unmarshal(cmd.Data, &hub.HubInfo); err != nil {
					return &hub, err
				}
				fixVersion()
				stage = optStatus
			case optStatus:
				// optionally wait for status command
				if cmd.Name == (ChatMessage{}).Cmd() {
					continue // ignore
				}
				if cmd.Name != (Status{}).Cmd() {
					return &hub, fmt.Errorf("expected status, received: %#v", cmd)
				}
				var st Status
				if err := Unmarshal(cmd.Data, &st); err != nil {
					return &hub, err
				} else if !st.Ok() {
					return &hub, st.Err()
				}
				stage = userList
			case userList:
				if cmd.Name != (Disconnect{}).Cmd() {
					return &hub, fmt.Errorf("expected disconnect, received: %#v", cmd)
				}
				var st Disconnect
				if err := Unmarshal(cmd.Data, &st); err != nil {
					return &hub, err
				}
				e := Error{Status{Sev: Fatal, Msg: st.Message}}
				if strings.Contains(st.Message, "registered users only") {
					e.Code = 26
				}
				if e.Msg == "" {
					e.Msg = "hub closed the connection"
				}
				return &hub, e
			default:
				return &hub, fmt.Errorf("unexpected command in stage %d: %#v", stage, cmd)
			}
			continue
		case *BroadcastPacket:
			switch stage {
			case optStatus:
				stage = userList
				fallthrough
			case userList:
				if cmd.ID == sid.SID {
					// make sure to wipe PID, so we don't send it later occasionally
					user.Pid = nil
					if err := Unmarshal(cmd.Data, &user); err != nil {
						return &hub, err
					}
					// done, should switch to NORMAL
					return &hub, nil
				}
				// other users
				var u User
				if err := Unmarshal(cmd.Data, &u); err != nil {
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
				return &hub, fmt.Errorf("unexpected command in stage %d: %#v", stage, cmd)
			}
		default:
			return &hub, fmt.Errorf("unexpected command: %#v", cmd)
		}
	}
}

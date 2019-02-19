package adc

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/direct-connect/go-dcpp/adc/types"
)

type PingHubInfo struct {
	Ext     []string
	HubInfo HubInfo
	Users   []User
}

//ctx context.Context

func Ping(ctx context.Context, addr string) (*PingHubInfo, error) {
	if i := strings.Index(addr, "://"); i >= 0 {
		proto := addr[:i]
		addr = addr[i+3:]
		switch proto {
		case SchemaADC, SchemaADCS:
			// continue
		default:
			return nil, fmt.Errorf("unsupported protocol: %q", proto)
		}
	}
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	c, err := NewConn(conn)
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

		FeaBZIP: true,
		// TODO: ZLIG
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
	num := int64(time.Now().Nanosecond())
	user := User{
		Id:       pid.Hash(),
		Pid:      &pid,
		Name:     "pinger_" + strconv.FormatInt(num, 16),
		Features: ext,
		Slots:    1,
	}

	err = c.WriteBroadcast(sid.SID, user)
	if err != nil {
		return nil, err
	}
	if err := c.Flush(); err != nil {
		return nil, err
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
		if err != nil {
			return nil, err
		}
		switch cmd := cmd.(type) {
		case *InfoPacket:
			switch stage {
			case hubInfo:
				// waiting for hub info
				if cmd.Name != (User{}).Cmd() {
					return nil, fmt.Errorf("expected hub info, received: %#v", cmd.Data)
				}
				if err := Unmarshal(cmd.Data, &hub.HubInfo); err != nil {
					return nil, err
				}
				stage = optStatus
			case optStatus:
				// optionally wait for status command
				if cmd.Name != (Status{}).Cmd() {
					return nil, fmt.Errorf("expected status, received: %#v", cmd)
				}
				var st Status
				if err := Unmarshal(cmd.Data, &st); err != nil {
					return nil, err
				} else if !st.Ok() {
					return nil, st.Err()
				}
				stage = userList
			default:
				return nil, fmt.Errorf("unexpected command in stage %d: %#v", stage, cmd)
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
						return nil, err
					}
					// done, should switch to NORMAL
					return nil, nil
				}
				// other users
				var u User
				if err := Unmarshal(cmd.Data, &u); err != nil {
					return nil, err
				}
				hub.Users = append(hub.Users, u)
				// continue until we see ourselves in the list
			default:
				return nil, fmt.Errorf("unexpected command in stage %d: %#v", stage, cmd)
			}
		default:
			return nil, fmt.Errorf("unexpected command: %#v", cmd)
		}
	}
}

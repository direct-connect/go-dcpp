package nmdc

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dennwc/go-dcpp/version"
)

type Software struct {
	Name string
	Vers string
}

type HubInfo struct {
	Name   string
	Topic  string
	Server Software
	Ext    []string
	Users  []MyInfo
}

func Ping(ctx context.Context, addr string) (*HubInfo, error) {
	// TODO: use context
	c, err := Dial(addr)
	if err != nil {
		return nil, err
	}
	defer c.Close()

	// set dealine once
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(time.Second * 10)
	}
	if err = c.conn.SetDeadline(deadline); err != nil {
		return nil, err
	}

	num := int64(time.Now().Nanosecond())
	name := "pinger_" + strconv.FormatInt(num, 16)
	lock, err := c.SendClientHandshake(time.Time{}, name, FeaNoHello)
	if err != nil {
		return nil, err
	}
	var hub HubInfo

	// TODO: check if it's always the case
	pk := strings.SplitN(lock.PK, " ", 2)
	hub.Server.Name = pk[0]
	if len(pk) > 1 {
		hub.Server.Vers = pk[1]
	}

	for {
		msg, err := c.ReadMsg(time.Time{})
		if err != nil {
			return nil, err
		}
		switch msg := msg.(type) {
		case *Supports:
			hub.Ext = msg.Ext
		case *HubName:
			hub.Name = string(msg.Name)
		case *HubTopic:
			hub.Topic = msg.Text
		case *Hello:
			// TODO: assumes NoHello
			if string(msg.Name) != name {
				return nil, fmt.Errorf("unexpected name in hello: %q", msg.Name)
			}
			err = c.SendClientInfo(time.Time{}, &MyInfo{
				Name:    Name(name),
				Client:  version.Name,
				Version: version.Vers,
				Mode:    UserModePassive,
				Hubs:    [3]int{1, 0, 0},
				Slots:   1,
				Conn:    "Cable",
				Flag:    FlagStatusServer,
			})
			if err != nil {
				return nil, err
			}
		case *MyInfo:
			if string(msg.Name) == name {
				// TODO: this might not be the end of a user list
				return &hub, nil
			}
			hub.Users = append(hub.Users, *msg)
		}
	}
}

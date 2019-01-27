package nmdc

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	Name     string
	Addr     string
	Desc     string
	Topic    string
	Failover []string
	Encoding string
	Owner    string
	Server   Software
	Ext      []string
	Users    []MyInfo
	Ops      []string
	Bots     []string
}

type timeoutErr interface {
	Timeout() bool
}

func Ping(ctx context.Context, addr string) (*HubInfo, error) {
	addr, err := NormalizeAddr(addr)
	if err != nil {
		return nil, err
	}

	// TODO: use context
	c, err := Dial(addr)
	if err != nil {
		return nil, err
	}
	defer c.Close()

	// set deadline once
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(time.Second * 10)
	}
	if err = c.conn.SetDeadline(deadline); err != nil {
		return nil, err
	}

	num := int64(time.Now().Nanosecond())
	name := "pinger_" + strconv.FormatInt(num, 16)
	lock, err := c.SendClientHandshake(time.Time{}, name,
		// dump most extensions we know to probe the for support of them
		FeaNoHello, FeaNoGetINFO, FeaTLS, FeaUserIP2,
		FeaUserCommand, FeaTTHSearch, FeaADCGet,
		FeaBotINFO, FeaHubINFO, FeaBotList,
		FeaMCTo, FeaNickChange, FeaClientNick,
		FeaIN, FeaFeaturedNetworks, FeaGetZBlock, FeaClientID,
		FeaXmlBZList, FeaMinislots, FeaTTHL, FeaTTHF,
		FeaZLIG, FeaACTM, FeaBZList, FeaSaltPass, FeaDHT0,
		FeaFailOver, FeaHubTopic, FeaOpPlus,
		FeaBanMsg, FeaNickRule, FeaSearchRule, FeaExtJSON2,
		// proposals
		FeaLocale,

		//FeaQuickList, // some hubs doesn't like this
	)
	if err != nil {
		return nil, err
	}

	if err = c.conn.SetDeadline(deadline); err != nil {
		return nil, err
	}

	var hub HubInfo

	// TODO: check if it's always the case
	pk := strings.SplitN(lock.PK, " ", 2)
	hub.Server.Name = pk[0]
	if len(pk) > 1 {
		hub.Server.Vers = pk[1]
	}

	var (
		lastMsg     string
		listStarted bool
		listEnd     bool
	)
	for {
		msg, err := c.ReadMsg(time.Time{})
		if err == io.EOF {
			if lastMsg != "" {
				return nil, fmt.Errorf("connection closed: %s", lastMsg)
			}
			return nil, errors.New("connection closed")
		} else if e, ok := err.(timeoutErr); ok && e.Timeout() && listStarted {
			return &hub, nil
		} else if err != nil {
			return nil, err
		}
		if listStarted {
			_, ok := msg.(*MyInfo)
			if !ok {
				listEnd = true
			}
		}
		switch msg := msg.(type) {
		case *ChatMessage:
			// we save the last message since it usually describes
			// an error before hub drops the connection
			lastMsg = string(msg.Text)
		case *Supports:
			hub.Ext = msg.Ext
		case *HubName:
			if hub.Name == "" {
				hub.Name = string(msg.Name)
			}
		case *HubTopic:
			hub.Topic = msg.Text
		case *Hello:
			// TODO: assumes NoHello
			if string(msg.Name) != name {
				return nil, fmt.Errorf("unexpected name in hello: %q", msg.Name)
			}
			err = c.SendPingerInfo(time.Time{}, &MyInfo{
				Name:      Name(name),
				Client:    version.Name,
				Version:   version.Vers,
				Mode:      UserModePassive,
				Hubs:      [3]int{1, 0, 0},
				Slots:     1,
				ShareSize: 13 * 1023 * 1023 * 1023,
				Conn:      "Cable",
				Flag:      FlagStatusServer,
			})
			if err != nil {
				return nil, err
			}
		case *Quit:
			// status update - we are definitely done
			return &hub, nil
		case *MyInfo:
			if listEnd {
				// if we receive it again after the list ended, it's really
				// an update, not a part of the list, so we can safely exit
				return &hub, nil
			} else if string(msg.Name) != name {
				listStarted = true
			}
			hub.Users = append(hub.Users, *msg)
		case *OpList:
			var arr []string
			for _, name := range msg.List {
				arr = append(arr, string(name))
			}
			hub.Ops = arr
		case *BotList:
			var arr []string
			for _, name := range msg.List {
				arr = append(arr, string(name))
			}
			hub.Bots = arr
		case *HubINFO:
			if msg.Desc != "" {
				hub.Desc = string(msg.Desc)
			}
			if msg.Host != "" {
				hub.Addr = msg.Host
			}
			if msg.Soft != "" {
				hub.Server.Name = msg.Soft
				if i := strings.LastIndex(msg.Soft, " "); i > 0 {
					hub.Server.Name, hub.Server.Vers = msg.Soft[:i], msg.Soft[i+1:]
				}
			}
			hub.Encoding = msg.Encoding
			hub.Owner = msg.Owner
		case *RawCommand:
			switch msg.Name {
			case "UserIP":
				// TODO: some implementations seem to end the list with this message
			case "Failover":
				// TODO: implement
				hub.Failover = strings.Split(string(msg.Data), ",")
			}
		}
	}
}

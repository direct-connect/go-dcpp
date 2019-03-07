package nmdc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/htmlindex"

	"github.com/direct-connect/go-dcpp/version"
)

const (
	fakeSlots = 5
	fakeShare = 321 * 1023 * 1023 * 1023
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

func (h *HubInfo) decodeWith(enc encoding.Encoding) error {
	dec := enc.NewDecoder()
	for _, ptr := range []*string{
		&h.Name,
		&h.Desc,
		&h.Topic,
		&h.Owner,
	} {
		s, err := dec.String(*ptr)
		if err != nil {
			return err
		}
		*ptr = s
	}
	for i := range h.Users {
		u := &h.Users[i]
		s, err := dec.String(string(u.Name))
		if err != nil {
			return err
		}
		u.Name = Name(s)
		s, err = dec.String(string(u.Desc))
		if err != nil {
			return err
		}
		u.Desc = String(s)
	}
	return nil
}

func (h *HubInfo) decode() error {
	if strings.Contains(h.Encoding, "@") {
		// YnHub may send an email in the encoding field
		if h.Owner == "" {
			h.Owner = h.Encoding
		}
		h.Encoding = ""
		return nil
	}
	code := strings.ToLower(h.Encoding)
	// some hubs are misconfigured and send garbage in the encoding field
	code = strings.Trim(code, "= ")
	if h.Encoding == "" {
		return nil
	}
	if len(code) == 4 {
		// some hubs forget the "cp" prefix and just use "1250" as an encoding
		if _, err := strconv.Atoi(code); err == nil {
			code = "cp" + code
		}
	}
	enc, err := htmlindex.Get(code)
	if err != nil {
		return nil
	}
	err = h.decodeWith(enc)
	if err != nil {
		return nil
	}
	if code, _ = htmlindex.Name(enc); code != "" {
		h.Encoding = code
	}
	return nil
}

type timeoutErr interface {
	Timeout() bool
}

func Ping(ctx context.Context, addr string) (_ *HubInfo, gerr error) {
	addr, err := NormalizeAddr(addr)
	if err != nil {
		return nil, err
	}

	c, err := DialContext(ctx, addr)
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
		FeaZLIG, FeaACTM, FeaBZList, FeaSaltPass,
		FeaFailOver, FeaHubTopic, FeaOpPlus,
		FeaBanMsg, FeaNickRule, FeaSearchRule, FeaExtJSON2,
		// proposals
		FeaLocale,

		//FeaQuickList, // some hubs doesn't like this
		//FeaDHT0, // some hubs ask users to disable it and drops the connection
	)
	if err != nil {
		return nil, err
	}

	if err = c.conn.SetDeadline(deadline); err != nil {
		return nil, err
	}

	var hub HubInfo
	defer func() {
		// make sure to decode the info at the end
		if gerr == nil {
			gerr = hub.decode()
		}
	}()

	// TODO: check if it's always the case
	pk := strings.SplitN(lock.PK, " ", 2)
	hub.Server.Name = pk[0]
	if len(pk) > 1 {
		hub.Server.Vers = pk[1]
	} else if strings.HasPrefix(pk[0], "version") {
		// old versions of Verlihub
		if i := strings.LastIndex(lock.Lock, "_"); i >= 0 {
			hub.Server.Name = lock.Lock[i+1:]
			hub.Server.Vers = strings.TrimPrefix(pk[0], "version")
		}
	}

	var (
		lastMsg     string
		listStarted bool
		listEnd     bool
		poweredBy   bool
	)
	for {
		msg, err := c.ReadMsg(time.Time{})
		if err == io.EOF {
			if listStarted || listEnd {
				return &hub, nil
			}
			if lastMsg != "" {
				return &hub, fmt.Errorf("connection closed: %s", lastMsg)
			}
			return &hub, errors.New("connection closed")
		} else if e, ok := err.(timeoutErr); ok && e.Timeout() && listStarted {
			return &hub, nil
		} else if err != nil {
			return &hub, err
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
			lastMsg = msg.Text

			if poweredBy { // YnHub and PtokaX version check
				poweredBy = false
				for _, pref := range []string{
					"YnHub version: ",
					"PtokaX DC Hub ",
					"Archos DC Hub ",
				} {
					if i := strings.Index(lastMsg, pref); i >= 0 {
						vers := lastMsg[i+len(pref):]
						if i = strings.Index(vers, " "); i >= 0 {
							vers = vers[:i]
						}
						hub.Server.Vers = vers
						break
					}
				}
			}
		case *Supports:
			hub.Ext = msg.Ext
		case *HubName:
			if hub.Name == "" {
				hub.Name = string(msg.String)
			}
			poweredBy = true
		case *HubTopic:
			hub.Topic = msg.Text
		case *Hello:
			// TODO: assumes NoHello
			if string(msg.Name) != name {
				return &hub, fmt.Errorf("unexpected name in hello: %q", msg.Name)
			}
			err = c.SendPingerInfo(time.Time{}, &MyInfo{
				Name:           Name(name),
				Client:         version.Name,
				Version:        version.Vers,
				Mode:           UserModeActive,
				HubsNormal:     1,
				HubsRegistered: 1,
				HubsOperator:   0,
				Slots:          fakeSlots,
				ShareSize:      fakeShare,
				Conn:           "Cable",
				Flag:           FlagStatusServer,
			})
			if err != nil {
				return &hub, err
			}
		case *Quit, *ConnectToMe, *RevConnectToMe, *Search:
			// status update, connection attempt or search - we are definitely done
			return &hub, nil
		case *MyInfo:
			if listEnd {
				// if we receive it again after the list ended, it's really
				// an update, not a part of the list, so we can safely exit
				return &hub, nil
			}
			if string(msg.Name) != name {
				listStarted = true
				hub.Users = append(hub.Users, *msg)
			}
		case *OpList:
			for _, name := range msg.Names {
				hub.Ops = append(hub.Ops, string(name))
			}
		case *BotList:
			var arr []string
			for _, name := range msg.Names {
				arr = append(arr, string(name))
			}
			hub.Bots = arr
		case *HubINFO:
			if msg.Name != "" {
				hub.Name = string(msg.Name)
			}
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
				} else if i = strings.LastIndex(msg.Soft, "_"); i > 0 {
					hub.Server.Name, hub.Server.Vers = msg.Soft[:i], msg.Soft[i+1:]
				}
			}
			// will be decoded later, see defer
			hub.Encoding = msg.Encoding
			hub.Owner = msg.Owner
		case *FailOver:
			hub.Failover = append(hub.Failover, msg.Host...)
		case *UserCommand:
			if hub.Server.Name == "YnHub" {
				// TODO: check if it's true for other hubs
				return &hub, nil
			}
		case *UserIP:
			// TODO: some implementations seem to end the list with this message
		}
	}
}

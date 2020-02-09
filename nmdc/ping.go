package nmdc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/htmlindex"

	"github.com/direct-connect/go-dc/nmdc"
	"github.com/direct-connect/go-dc/types"
	"github.com/direct-connect/go-dcpp/version"
)

var (
	ErrRegisteredOnly = errors.New("registered users only")
)

type ErrBanned struct {
	Reason string
}

func (e *ErrBanned) Error() string {
	if e.Reason == "" {
		return "banned"
	}
	return fmt.Sprintf("banned: %q", e.Reason)
}

type HubInfo struct {
	Name      string
	Addr      string
	KeyPrints []string
	Desc      string
	Topic     string
	Failover  []string
	Encoding  string
	Owner     string
	Server    types.Software
	Ext       []string
	Users     []nmdc.MyINFO
	Ops       []string
	Bots      []string
	Redirect  string
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
		u.Name = s
		s, err = dec.String(string(u.Desc))
		if err != nil {
			return err
		}
		u.Desc = s
	}
	return nil
}

type timeoutErr interface {
	Timeout() bool
}

type PingConfig struct {
	Name  string
	Share uint64
	Slots int
	Hubs  int
}

func Ping(ctx context.Context, addr string, conf PingConfig) (_ *HubInfo, gerr error) {
	if conf.Name == "" {
		return nil, errors.New("pinger name should be set")
	}
	addr, err := nmdc.NormalizeAddr(addr)
	if err != nil {
		return nil, err
	}

	var hubInfo []byte
	c, err := DialContext(ctx, addr)
	if err != nil {
		return nil, err
	}
	defer c.Close()

	c.r.OnRawMessage(func(cmd, args []byte) (bool, error) {
		if bytes.Equal(cmd, []byte("HubINFO")) {
			hubInfo = append([]byte{}, args...)
		} else if bytes.Equal(cmd, []byte("UserCommand")) {
			return false, nil
		}
		return true, nil
	})

	// set deadline once
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(time.Second * 10)
	}
	if err = c.conn.SetDeadline(deadline); err != nil {
		return nil, err
	}

	lock, err := c.SendClientHandshake(time.Time{},
		// dump most extensions we know to probe the hub for support of them
		nmdc.ExtNoHello, nmdc.ExtNoGetINFO, nmdc.ExtTLS, nmdc.ExtUserIP2,
		nmdc.ExtUserCommand, nmdc.ExtTTHSearch, nmdc.ExtZPipe0, nmdc.ExtADCGet,
		nmdc.ExtBotINFO, nmdc.ExtHubINFO, nmdc.ExtBotList,
		nmdc.ExtMCTo, nmdc.ExtNickChange, nmdc.ExtClientNick,
		nmdc.ExtIN, nmdc.ExtFeaturedNetworks, nmdc.ExtGetZBlock, nmdc.ExtClientID,
		nmdc.ExtXmlBZList, nmdc.ExtMinislots, nmdc.ExtTTHL, nmdc.ExtTTHF,
		nmdc.ExtZLIG, nmdc.ExtACTM, nmdc.ExtBZList, nmdc.ExtSaltPass,
		nmdc.ExtFailOver, nmdc.ExtHubTopic, nmdc.ExtOpPlus,
		nmdc.ExtBanMsg, nmdc.ExtNickRule, nmdc.ExtSearchRule, nmdc.ExtExtJSON2,
		// proposals
		nmdc.ExtLocale,

		//FeaQuickList, // some hubs doesn't like this
		//FeaDHT0, // some hubs ask users to disable it and drops the connection
	)
	if e, ok := err.(*nmdc.ErrUnexpectedCommand); ok {
		// chat message: it may be a ban
		if e.Received.Typ == "" {
			var m nmdc.ChatMessage
			_ = m.UnmarshalNMDC(nil, e.Received.Data)
			if strings.Contains(m.Text, "Banned by") {
				reason := ""
				if i := strings.Index(m.Text, "Reason: "); i > 0 {
					reason = m.Text[i+8:]
					if i := strings.IndexByte(reason, '\n'); i > 0 {
						reason = reason[:i]
					}
					reason = strings.TrimSpace(reason)
				}
				return nil, &ErrBanned{Reason: reason}
			}
		}
	}
	if err != nil {
		return nil, err
	}

	if err = c.conn.SetDeadline(deadline); err != nil {
		return nil, err
	}

	var hub HubInfo
	hub.KeyPrints = c.GetKeyPrints()

	// TODO: check if it's always the case
	pk := strings.SplitN(lock.PK, " ", 2)
	hub.Server.Name = pk[0]
	if len(pk) > 1 {
		hub.Server.Version = pk[1]
	} else if strings.HasPrefix(pk[0], "version") {
		hub.Server.Version = "v" + strings.TrimPrefix(pk[0], "version")
		if strings.Contains(lock.Lock, "FLEXHUB_MULTIPROTOCOL") {
			hub.Server.Name = "FlexHub"
		} else if i := strings.LastIndex(lock.Lock, "_"); i >= 0 {
			// old versions of Verlihub
			hub.Server.Name = lock.Lock[i+1:]
		}
	}

	setInfo := func(msg *nmdc.HubINFO, enc string) {
		if msg.Name != "" {
			hub.Name = msg.Name
		}
		if msg.Desc != "" {
			hub.Desc = msg.Desc
		}
		if msg.Host != "" {
			hub.Addr = msg.Host
		}
		if msg.Soft.Name != "" {
			hub.Server.Name = msg.Soft.Name
			if msg.Soft.Version != "" {
				hub.Server.Version = msg.Soft.Version
			} else {
				soft := msg.Soft.Name
				if i := strings.LastIndex(soft, " "); i > 0 {
					hub.Server = types.Software{
						Name:    soft[:i],
						Version: soft[i+1:],
					}
				} else if i = strings.LastIndex(soft, "_"); i > 0 {
					hub.Server = types.Software{
						Name:    soft[:i],
						Version: soft[i+1:],
					}
				}
			}
		}
		if enc != "" {
			hub.Encoding = enc
		} else {
			hub.Encoding = msg.Encoding
		}
		hub.Owner = msg.Owner
	}

	switchEncoding := func(msg *nmdc.HubINFO) {
		if strings.Contains(hub.Encoding, "@") {
			// YnHub may send an email in the encoding field
			if msg.Owner == "" {
				msg.Owner = msg.Encoding
			}
			msg.Encoding = ""
			setInfo(msg, "")
			return
		}
		code := strings.ToLower(msg.Encoding)
		// some hubs are misconfigured and send garbage in the encoding field
		code = strings.Trim(code, "= ")
		if code == "" {
			msg.Encoding = ""
			setInfo(msg, "")
			return
		}
		if len(code) == 4 {
			// some hubs forget the "cp" prefix and just use "1250" as an encoding
			if _, err := strconv.Atoi(code); err == nil {
				code = "cp" + code
			}
		}
		enc, err := htmlindex.Get(code)
		if err != nil {
			setInfo(msg, "")
			return
		}

		var msg2 nmdc.HubINFO
		err = msg2.UnmarshalNMDC(enc.NewDecoder(), hubInfo)
		if err != nil {
			setInfo(msg, "")
			return
		}
		if c, _ := htmlindex.Name(enc); c != "" {
			code = c
		}
		setInfo(&msg2, code)
		c.SetEncoding(enc)
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
			if hub.Redirect != "" {
				return &hub, nil
			}
			if lastMsg != "" {
				err = fmt.Errorf("connection closed: %s", lastMsg)
				if strings.Contains(lastMsg, "registered users only") {
					err = ErrRegisteredOnly
				}
				return &hub, err
			}
			return &hub, errors.New("connection closed")
		} else if e, ok := err.(timeoutErr); ok && e.Timeout() && listStarted {
			return &hub, nil
		} else if err != nil {
			return &hub, err
		}
		if listStarted {
			_, ok := msg.(*nmdc.MyINFO)
			if !ok {
				listEnd = true
			}
		}
		switch msg := msg.(type) {
		case *nmdc.ChatMessage:
			// we save the last message since it usually describes
			// an error before hub drops the connection
			lastMsg = msg.Text

			if poweredBy { // YnHub and PtokaX version check
				poweredBy = false
				for _, pref := range []string{
					"YnHub version: ",
					"PtokaX DC Hub ",
					"Archos DC Hub ",
					"SunGateDCH by Meloun ",
				} {
					if i := strings.Index(lastMsg, pref); i >= 0 {
						vers := lastMsg[i+len(pref):]
						if i = strings.Index(vers, " "); i >= 0 {
							vers = vers[:i]
						}
						hub.Server.Version = vers
						break
					}
				}
			}
		case *nmdc.Supports:
			hub.Ext = msg.Ext
			err = c.WriteMsg(&nmdc.ValidateNick{Name: nmdc.Name(conf.Name)})
			if err != nil {
				return nil, err
			}
			err = c.Flush()
			if err != nil {
				return nil, err
			}
		case *nmdc.HubName:
			if hub.Name == "" {
				hub.Name = string(msg.String)
			}
			poweredBy = true
		case *nmdc.HubTopic:
			hub.Topic = msg.Text
		case *nmdc.Hello:
			// TODO: assumes NoHello
			if string(msg.Name) != conf.Name {
				return &hub, fmt.Errorf("unexpected name in hello: %q", msg.Name)
			}
			err = c.SendPingerInfo(time.Time{}, &nmdc.MyINFO{
				Name: conf.Name,
				Client: types.Software{
					Name:    version.Name,
					Version: version.Vers,
				},
				Mode:           nmdc.UserModeActive,
				HubsNormal:     conf.Hubs,
				HubsRegistered: 1,
				HubsOperator:   0,
				Slots:          conf.Slots,
				ShareSize:      conf.Share,
				Conn:           "100",
				Flag:           nmdc.FlagStatusServer,
			})
			if err != nil {
				return &hub, err
			}
		case *nmdc.Quit, *nmdc.ConnectToMe, *nmdc.RevConnectToMe, *nmdc.Search:
			// status update, connection attempt or search - we are definitely done
			return &hub, nil
		case *nmdc.MyINFO:
			if listEnd {
				// if we receive it again after the list ended, it's really
				// an update, not a part of the list, so we can safely exit
				return &hub, nil
			}
			if string(msg.Name) != conf.Name {
				listStarted = true
				hub.Users = append(hub.Users, *msg)
			}
		case *nmdc.OpList:
			hub.Ops = msg.Names
		case *nmdc.BotList:
			hub.Bots = msg.Names
		case *nmdc.HubINFO:
			switchEncoding(msg)
		case *nmdc.FailOver:
			hub.Failover = append(hub.Failover, msg.Host...)
		case *nmdc.UserCommand:
			if hub.Server.Name == "YnHub" {
				// TODO: check if it's true for other hubs
				return &hub, nil
			}
		case *nmdc.UserIP:
		// TODO: some implementations seem to end the list with this message
		case *nmdc.ForceMove:
			hub.Redirect = string(msg.Address)
		}
	}
}

package nmdc

import (
	"errors"
	"strings"
	"time"
)

func (c *Conn) SendClientHandshake(deadline time.Time, name string, ext ...string) (*Lock, error) {
	var lock Lock
	err := c.ReadMsgTo(deadline, &lock)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(lock.Lock, "EXTENDEDPROTOCOL") {
		// TODO: support legacy protocol, if we care
		return nil, errors.New("legacy protocol is not supported")
	}
	err = c.WriteMsg(&Supports{Ext: ext})
	if err != nil {
		return nil, err
	}
	err = c.WriteMsg(lock.Key())
	if err != nil {
		return nil, err
	}
	err = c.WriteMsg(&ValidateNick{Name: Name(name)})
	if err != nil {
		return nil, err
	}
	err = c.Flush()
	if err != nil {
		return nil, err
	}
	return &lock, nil
}

func (c *Conn) SendClientInfo(deadline time.Time, info *MyInfo) error {
	err := c.WriteMsg(&Version{Vers: "1,0091"})
	if err != nil {
		return err
	}
	err = c.WriteMsg(&GetNickList{})
	if err != nil {
		return err
	}
	err = c.WriteMsg(info)
	if err != nil {
		return err
	}
	return c.Flush()
}

func (c *Conn) SendPingerInfo(deadline time.Time, info *MyInfo) error {
	err := c.WriteMsg(&RawCommand{Name: "BotINFO", Data: []byte(info.Name)})
	if err != nil {
		return err
	}
	return c.SendClientInfo(deadline, info)
}

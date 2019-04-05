package nmdc

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/direct-connect/go-dc/nmdc"
)

func (c *Conn) SendClientHandshake(deadline time.Time, ext ...string) (*nmdc.Lock, error) {
	var lock nmdc.Lock
	err := c.ReadMsgTo(deadline, &lock)
	if err == io.EOF {
		return nil, io.ErrUnexpectedEOF
	} else if err != nil {
		return nil, err
	}
	if lock.NoExt {
		// TODO: support legacy protocol, if we care
		return nil, errors.New("legacy protocol is not supported")
	}
	err = c.WriteMsg(&nmdc.Supports{Ext: ext})
	if err != nil {
		return nil, err
	}
	err = c.WriteMsg(lock.Key())
	if err != nil {
		return nil, err
	}
	err = c.Flush()
	if err != nil {
		return nil, err
	}
	return &lock, nil
}

func (c *Conn) SendClientInfo(deadline time.Time, info *nmdc.MyINFO) error {
	err := c.WriteMsg(&nmdc.Version{Vers: "1,0091"})
	if err != nil {
		return err
	}
	err = c.WriteMsg(&nmdc.GetNickList{})
	if err != nil {
		return err
	}
	err = c.WriteMsg(info)
	if err != nil {
		return err
	}
	return c.Flush()
}

func (c *Conn) SendPingerInfo(deadline time.Time, info *nmdc.MyINFO) error {
	err := c.WriteMsg(&nmdc.BotINFO{String: nmdc.String(info.Name)})
	if err != nil {
		return err
	}
	return c.SendClientInfo(deadline, info)
}

func (c *Conn) ReadValidateNick(deadline time.Time) (*nmdc.ValidateNick, error) {
	var nick nmdc.ValidateNick
	err := c.ReadMsgTo(deadline, &nick)
	if err != nil {
		return nil, fmt.Errorf("expected validate: %v", err)
	}
	return &nick, nil
}

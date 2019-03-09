package nmdc

import (
	"errors"
	"fmt"
	"io"
	"time"
	"unicode/utf8"

	"github.com/direct-connect/go-dc/nmdc"
)

func (c *Conn) SendClientHandshake(deadline time.Time, name string, ext ...string) (*nmdc.Lock, error) {
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
	err = c.WriteMsg(&nmdc.ValidateNick{Name: nmdc.Name(name)})
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
	if c.encoding != nil || c.fallback == nil || utf8.ValidString(string(nick.Name)) {
		return &nick, nil
	}
	// try fallback encoding
	dec := c.fallback.NewDecoder()
	str, err := dec.String(string(nick.Name))
	if err != nil || !utf8.ValidString(str) {
		// give up
		return &nick, nil
	}
	// success - switch to this encoding
	nick.Name = nmdc.Name(str)
	c.SetEncoding(c.fallback)
	return &nick, nil
}

func (c *Conn) ReadMyInfoTo(deadline time.Time, info *nmdc.MyINFO) error {
	err := c.ReadMsgTo(deadline, info)
	if err != nil {
		return fmt.Errorf("expected user info: %v", err)
	}
	if c.encoding != nil || c.fallback == nil {
		return nil
	} else if utf8.ValidString(string(info.Name)) && utf8.ValidString(string(info.Desc)) {
		return nil
	}
	// try fallback encoding
	dec := c.fallback.NewDecoder()
	name, err := dec.String(string(info.Name))
	if err != nil || !utf8.ValidString(name) {
		return nil
	}
	desc, err := dec.String(string(info.Desc))
	if err != nil || !utf8.ValidString(desc) {
		return nil
	}
	// fallback is valid, switch encoding
	info.Name = name
	info.Desc = desc
	c.SetEncoding(c.fallback)
	return nil
}

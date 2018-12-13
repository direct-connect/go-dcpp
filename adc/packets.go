package adc

import (
	"fmt"
)

const (
	kindBroadcast = 'B'
	kindClient    = 'C'
	kindDirect    = 'D'
	kindEcho      = 'E'
	kindFeature   = 'F'
	kindHub       = 'H'
	kindInfo      = 'I'
	kindUDP       = 'U'
)

type MsgType [3]byte

func (s MsgType) String() string { return string(s[:]) }

type Packet interface {
	kind() byte
	Message() RawMessage
	Decode() (Message, error)
	UnmarshalPacket(name MsgType, data []byte) error
	MarshalPacket() ([]byte, error)
}

type BasePacket struct {
	Name MsgType
	Data []byte
}

func (p BasePacket) Message() RawMessage {
	return RawMessage{Type: p.Name, Data: p.Data}
}
func (p BasePacket) Decode() (Message, error) {
	return UnmarshalMessage(p.Name, p.Data)
}

func DecodePacket(p []byte) (Packet, error) {
	if len(p) < 4 {
		return nil, fmt.Errorf("too short for command: '%s'", string(p))
	}
	kind := p[0]
	var m Packet
	switch kind {
	case kindInfo:
		m = &InfoPacket{}
	case kindHub:
		m = &HubPacket{}
	case kindEcho:
		m = &EchoPacket{}
	case kindDirect:
		m = &DirectPacket{}
	case kindBroadcast:
		m = &BroadcastPacket{}
	case kindFeature:
		m = &FeaturePacket{}
	case kindClient:
		m = &ClientPacket{}
	case kindUDP:
		m = &UDPPacket{}
	default:
		return nil, fmt.Errorf("unknown command kind: %c", p[0])
	}
	var cname MsgType
	copy(cname[:], p[1:4])
	p = p[4:]
	var raw []byte
	if len(p) > 0 {
		if p[0] != ' ' {
			return nil, fmt.Errorf("name separator expected")
		}
		raw = p[1:]
	}
	if err := m.UnmarshalPacket(cname, raw); err != nil {
		return nil, err
	}
	return m, nil
}

var _ Packet = (*InfoPacket)(nil)

type InfoPacket struct {
	BasePacket
}

func (*InfoPacket) kind() byte {
	return kindInfo
}
func (p *InfoPacket) UnmarshalPacket(name MsgType, data []byte) error {
	p.Name = name
	p.Data = data
	return nil
}
func (p *InfoPacket) MarshalPacket() ([]byte, error) {
	// IINF <data>
	n := 4
	if len(p.Data) > 0 {
		n += 1 + len(p.Data)
	}
	buf := make([]byte, n)
	buf[0] = p.kind()
	copy(buf[1:], p.Name[:])
	if len(p.Data) > 0 {
		buf[4] = ' '
		copy(buf[5:], p.Data)
	}
	return buf, nil
}

var _ Packet = (*HubPacket)(nil)

type HubPacket struct {
	BasePacket
}

func (*HubPacket) kind() byte {
	return kindHub
}
func (p *HubPacket) UnmarshalPacket(name MsgType, data []byte) error {
	p.Name = name
	p.Data = data
	return nil
}
func (p *HubPacket) MarshalPacket() ([]byte, error) {
	// HINF <data>
	n := 4
	if len(p.Data) > 0 {
		n += 1 + len(p.Data)
	}
	buf := make([]byte, n)
	buf[0] = p.kind()
	copy(buf[1:], p.Name[:])
	if len(p.Data) > 0 {
		buf[4] = ' '
		copy(buf[5:], p.Data)
	}
	return buf, nil
}

var _ Packet = (*BroadcastPacket)(nil)

type BroadcastPacket struct {
	BasePacket
	ID SID
}

func (*BroadcastPacket) kind() byte {
	return kindBroadcast
}
func (p *BroadcastPacket) UnmarshalPacket(name MsgType, data []byte) error {
	if len(data) < 4 {
		return fmt.Errorf("short broadcast")
	} else if len(data) > 4 && data[4] != ' ' {
		return fmt.Errorf("separator expected: '%s'", string(data[:5]))
	}
	p.Name = name
	if err := p.ID.UnmarshalAdc(data[0:4]); err != nil {
		return err
	}
	if len(data) > 5 {
		p.Data = data[5:]
	}
	return nil
}
func (p *BroadcastPacket) MarshalPacket() ([]byte, error) {
	// BINF AAAA <data>
	n := 9
	if len(p.Data) > 0 {
		n += 1 + len(p.Data)
	}
	buf := make([]byte, n)
	buf[0] = p.kind()
	copy(buf[1:], p.Name[:])
	buf[4] = ' '
	id, _ := p.ID.MarshalAdc()
	copy(buf[5:], id[:])
	if len(p.Data) > 0 {
		buf[9] = ' '
		copy(buf[10:], p.Data)
	}
	return buf, nil
}

var _ Packet = (*DirectPacket)(nil)

type DirectPacket struct {
	BasePacket
	ID   SID
	Targ SID
}

func (DirectPacket) kind() byte {
	return kindDirect
}
func (p *DirectPacket) UnmarshalPacket(name MsgType, data []byte) error {
	if len(data) < 9 {
		return fmt.Errorf("short direct command")
	} else if data[4] != ' ' {
		return fmt.Errorf("separator expected: '%s'", string(data[:9]))
	} else if len(data) > 9 && data[9] != ' ' {
		return fmt.Errorf("separator expected: '%s'", string(data[:10]))
	}
	p.Name = name
	if err := p.ID.UnmarshalAdc(data[0:4]); err != nil {
		return err
	}
	if err := p.Targ.UnmarshalAdc(data[5:9]); err != nil {
		return err
	}
	if len(data) > 10 {
		p.Data = data[10:]
	}
	return nil
}
func (p DirectPacket) MarshalPacket() ([]byte, error) {
	// DCTM AAAA BBBB <data>
	n := 14
	if len(p.Data) > 0 {
		n += 1 + len(p.Data)
	}
	buf := make([]byte, n)
	buf[0] = p.kind()
	copy(buf[1:], p.Name[:])
	buf[4] = ' '
	id, _ := p.ID.MarshalAdc()
	copy(buf[5:], id[:])
	buf[9] = ' '
	targ, _ := p.Targ.MarshalAdc()
	copy(buf[10:], targ[:])
	if len(p.Data) > 0 {
		buf[14] = ' '
		copy(buf[15:], p.Data)
	}
	return buf, nil
}

var _ Packet = (*EchoPacket)(nil)

type EchoPacket struct {
	BasePacket
	ID   SID
	Targ SID
}

func (*EchoPacket) kind() byte {
	return kindEcho
}
func (p *EchoPacket) UnmarshalPacket(name MsgType, data []byte) error {
	if len(data) < 9 {
		return fmt.Errorf("short echo command")
	} else if data[4] != ' ' {
		return fmt.Errorf("separator expected: '%s'", string(data[:9]))
	} else if len(data) > 9 && data[9] != ' ' {
		return fmt.Errorf("separator expected: '%s'", string(data[:10]))
	}
	p.Name = name
	if err := p.ID.UnmarshalAdc(data[0:4]); err != nil {
		return err
	}
	if err := p.Targ.UnmarshalAdc(data[5:9]); err != nil {
		return err
	}
	if len(data) > 10 {
		p.Data = data[10:]
	}
	return nil
}
func (p *EchoPacket) MarshalPacket() ([]byte, error) {
	// EMSG AAAA BBBB <data>
	n := 14
	if len(p.Data) > 0 {
		n += 1 + len(p.Data)
	}
	buf := make([]byte, n)
	buf[0] = p.kind()
	copy(buf[1:], p.Name[:])
	buf[4] = ' '
	id, _ := p.ID.MarshalAdc()
	copy(buf[5:], id[:])
	buf[9] = ' '
	targ, _ := p.Targ.MarshalAdc()
	copy(buf[10:], targ[:])
	if len(p.Data) > 0 {
		buf[14] = ' '
		copy(buf[15:], p.Data)
	}
	return buf, nil
}

var _ Packet = (*ClientPacket)(nil)

type ClientPacket struct {
	BasePacket
}

func (*ClientPacket) kind() byte {
	return kindClient
}
func (p *ClientPacket) UnmarshalPacket(name MsgType, data []byte) error {
	p.Name = name
	p.Data = data
	return nil
}
func (p *ClientPacket) MarshalPacket() ([]byte, error) {
	// CINF <data>
	n := 4
	if len(p.Data) > 0 {
		n += 1 + len(p.Data)
	}
	buf := make([]byte, n)
	buf[0] = p.kind()
	copy(buf[1:], p.Name[:])
	if len(p.Data) > 0 {
		buf[4] = ' '
		copy(buf[5:], p.Data)
	}
	return buf, nil
}

var _ Packet = (*FeaturePacket)(nil)

type FeaturePacket struct {
	BasePacket
	ID       SID
	Features map[Feature]bool
}

func (*FeaturePacket) kind() byte {
	return kindFeature
}
func (p *FeaturePacket) UnmarshalPacket(name MsgType, data []byte) error {
	if len(data) < 4 {
		return fmt.Errorf("short feature command")
	} else if len(data) > 4 && data[4] != ' ' {
		return fmt.Errorf("separator expected: '%s'", string(data[:5]))
	}
	p.Name = name
	p.Features = make(map[Feature]bool)
	if err := p.ID.UnmarshalAdc(data[0:4]); err != nil {
		return err
	}
	if len(data) > 5 {
		data = data[5:]
	} else {
		data = nil
	}
	for i := 0; i < len(data); i++ {
		if data[i] == '+' {
			if f := data[i:]; len(f) < 5 {
				return fmt.Errorf("short feature: '%s'", string(data[i:i+5]))
			}
			var fea Feature
			copy(fea[:], data[i+1:i+5])
			p.Features[fea] = true
			i += 4
		} else if data[i] == '-' {
			if f := data[i:]; len(f) < 5 {
				return fmt.Errorf("short feature: '%s'", string(data[i:i+5]))
			}
			var fea Feature
			copy(fea[:], data[i+1:i+5])
			p.Features[fea] = false
			i += 4
		} else if data[i] == ' ' {
			data = data[i:]
			i = 0
		} else {
			data = data[i:]
			break
		}
		if i+1 == len(data) {
			data = nil
		}
	}
	if len(p.Features) == 0 {
		p.Features = nil
	}
	if len(data) > 0 {
		p.Data = data
	}
	return nil
}
func (p *FeaturePacket) MarshalPacket() ([]byte, error) {
	// FSCH AAAA +SEGA -NAT0 <data>
	n := 9
	if len(p.Data) > 0 {
		n += 1 + len(p.Data)
	}
	for k := range p.Features {
		n += 2 + len(k)
	}
	buf := make([]byte, n)
	buf[0] = p.kind()
	copy(buf[1:], p.Name[:])
	buf[4] = ' '
	id, _ := p.ID.MarshalAdc()
	copy(buf[5:], id[:])
	off := 9
	for k, v := range p.Features {
		buf[off] = ' '
		if v {
			buf[off+1] = '+'
		} else {
			buf[off+1] = '-'
		}
		n := copy(buf[off+2:], k[:])
		off += 2 + n
	}
	if len(p.Data) > 0 {
		buf[off] = ' '
		copy(buf[off+1:], p.Data)
	}
	return buf, nil
}

var _ Packet = (*UDPPacket)(nil)

type UDPPacket struct {
	BasePacket
	ID CID
}

func (*UDPPacket) kind() byte {
	return kindUDP
}
func (p *UDPPacket) UnmarshalPacket(name MsgType, data []byte) error {
	const l = 39 // len of CID in base32
	if len(data) < l {
		return fmt.Errorf("short upd command")
	} else if len(data) > l && data[l] != ' ' {
		return fmt.Errorf("separator expected: '%s'", string(data[:l+1]))
	}
	p.Name = name
	if err := p.ID.FromBase32(string(data[0:l])); err != nil {
		return fmt.Errorf("wrong CID in upd command: %v", err)
	}
	if len(data) > l+1 {
		p.Data = data[l+1:]
	}
	return nil
}
func (p *UDPPacket) MarshalPacket() ([]byte, error) {
	// UINF <CID> <data>
	n := 39 + 5
	if len(p.Data) > 0 {
		n += 1 + len(p.Data)
	}
	buf := make([]byte, n)
	buf[0] = p.kind()
	copy(buf[1:], p.Name[:])
	buf[4] = ' '
	copy(buf[5:], p.ID.ToBase32())
	if len(p.Data) > 0 {
		buf[5+39] = ' '
		copy(buf[5+39+1:], p.Data)
	}
	return buf, nil
}

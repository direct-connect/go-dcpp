package nmdc

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

var (
	messages = make(map[string]reflect.Type)
)

func init() {
	RegisterMessage(&Hello{})
	RegisterMessage(&Version{})
	RegisterMessage(&HubName{})
	RegisterMessage(&HubTopic{})
	RegisterMessage(&MyNick{})
	RegisterMessage(&ValidateNick{})
	RegisterMessage(&ValidateDenide{})
	RegisterMessage(&Quit{})
	RegisterMessage(&Lock{})
	RegisterMessage(&Key{})
	RegisterMessage(&Supports{})
	RegisterMessage(&GetNickList{})
	RegisterMessage(&MyInfo{})
	RegisterMessage(&OpList{})
	RegisterMessage(&BotList{})
	RegisterMessage(&ConnectToMe{})
	RegisterMessage(&RevConnectToMe{})
	RegisterMessage(&PrivateMessage{})
	RegisterMessage(&Failed{})
	RegisterMessage(&Error{})
}

type Message interface {
	Cmd() string
	MarshalNMDC() ([]byte, error)
	UnmarshalNMDC(data []byte) error
}

func RegisterMessage(m Message) {
	name := m.Cmd()
	if _, ok := messages[name]; ok {
		panic(fmt.Errorf("%q already registered", name))
	}
	rt := reflect.TypeOf(m)
	if rt.Kind() == reflect.Ptr {
		rt = rt.Elem()
	}
	messages[name] = rt
}

type RawCommand struct {
	Name string
	Data []byte
}

func (m *RawCommand) Cmd() string {
	return m.Name
}

func (m *RawCommand) MarshalNMDC() ([]byte, error) {
	return m.Data, nil
}

func (m *RawCommand) UnmarshalNMDC(data []byte) error {
	m.Data = data
	return nil
}

func (m *RawCommand) Decode() (Message, error) {
	rt, ok := messages[m.Name]
	if !ok {
		return m, nil
	}
	msg := reflect.New(rt).Interface().(Message)
	if err := msg.UnmarshalNMDC(m.Data); err != nil {
		return nil, err
	}
	return msg, nil
}

type ChatMessage struct {
	Name Name
	Text String
}

func (m *ChatMessage) Cmd() string {
	return "" // special case
}

func (m *ChatMessage) MarshalNMDC() ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	if m.Name != "" {
		buf.WriteByte('<')
		name, err := m.Name.MarshalNMDC()
		if err != nil {
			return nil, err
		}
		buf.Write(name)
		buf.WriteString("> ")
	}
	text, err := m.Text.MarshalNMDC()
	if err != nil {
		return nil, err
	}
	// TODO: convert to connection encoding
	buf.Write(text)
	buf.WriteByte('|')
	return buf.Bytes(), nil
}

func (m *ChatMessage) UnmarshalNMDC(data []byte) error {
	panic("special case")
}

type Hello struct {
	Name
}

func (*Hello) Cmd() string {
	return "Hello"
}

type HubName struct {
	Name
}

func (*HubName) Cmd() string {
	return "HubName"
}

type MyNick struct {
	Name
}

func (*MyNick) Cmd() string {
	return "MyNick"
}

type ValidateNick struct {
	Name
}

func (*ValidateNick) Cmd() string {
	return "ValidateNick"
}

type ValidateDenide struct {
	Name
}

func (*ValidateDenide) Cmd() string {
	return "ValidateDenide"
}

type Quit struct {
	Name
}

func (*Quit) Cmd() string {
	return "Quit"
}

type Version struct {
	Vers string
}

func (*Version) Cmd() string {
	return "Version"
}

func (m *Version) MarshalNMDC() ([]byte, error) {
	return []byte(m.Vers), nil
}

func (m *Version) UnmarshalNMDC(data []byte) error {
	m.Vers = string(data)
	return nil
}

type HubTopic struct {
	Text string
}

func (*HubTopic) Cmd() string {
	return "HubTopic"
}

func (m *HubTopic) MarshalNMDC() ([]byte, error) {
	return String(m.Text).MarshalNMDC()
}

func (m *HubTopic) UnmarshalNMDC(data []byte) error {
	var s String
	if err := s.UnmarshalNMDC(data); err != nil {
		return err
	}
	m.Text = string(s)
	return nil
}

type Lock struct {
	Lock string
	PK   string
	// TODO: Ref
}

func (*Lock) Cmd() string {
	return "Lock"
}

func (m *Lock) MarshalNMDC() ([]byte, error) {
	if len(m.PK) == 0 {
		return []byte(m.Lock), nil
	}
	return []byte(strings.Join([]string{
		m.Lock, " Pk=", m.PK,
	}, "")), nil
}

func (m *Lock) UnmarshalNMDC(data []byte) error {
	i := bytes.Index(data, []byte(" Pk="))
	if i >= 0 {
		m.PK = string(data[i+4:])
		data = data[:i]
	}
	m.Lock = string(data)
	return nil
}

type Key struct {
	Key string
}

func (*Key) Cmd() string {
	return "Key"
}

func (m *Key) MarshalNMDC() ([]byte, error) {
	return []byte(m.Key), nil
}

func (m *Key) UnmarshalNMDC(data []byte) error {
	m.Key = string(data)
	return nil
}

type Supports struct {
	Ext []string
}

func (*Supports) Cmd() string {
	return "Supports"
}

func (m *Supports) MarshalNMDC() ([]byte, error) {
	return []byte(strings.Join(m.Ext, " ")), nil
}

func (m *Supports) UnmarshalNMDC(data []byte) error {
	data = bytes.TrimSuffix(data, []byte(" "))
	m.Ext = strings.Split(string(data), " ")
	return nil
}

type GetNickList struct{}

func (*GetNickList) Cmd() string {
	return "GetNickList"
}

func (m *GetNickList) MarshalNMDC() ([]byte, error) {
	return nil, nil
}

func (m *GetNickList) UnmarshalNMDC(data []byte) error {
	// TODO: validate
	return nil
}

type UserMode byte

const (
	UserModeActive  = UserMode('A')
	UserModePassive = UserMode('P')
	UserModeSOCKS5  = UserMode('5')
)

type UserFlag byte

func (f UserFlag) IsSet(f2 UserFlag) bool {
	return f&f2 != 0
}

const (
	FlagStatusNormal   = UserFlag(0x01)
	FlagStatusAway     = UserFlag(0x02)
	FlagStatusServer   = UserFlag(0x04)
	FlagStatusFireball = UserFlag(0x08)
	FlagTLSDownload    = UserFlag(0x10)
	FlagTLSUpload      = UserFlag(0x20)
	FlagIPv4           = UserFlag(0x40)
	FlagIPv6           = UserFlag(0x80)

	FlagTLS = FlagTLSUpload | FlagTLSDownload
)

type MyInfo struct {
	Name      Name
	Desc      String
	Client    string
	Version   string
	Mode      UserMode
	Hubs      [3]int
	Slots     int
	Other     map[string]string
	Conn      string
	Flag      UserFlag
	Email     string
	ShareSize uint64
}

func (*MyInfo) Cmd() string {
	return "MyINFO"
}

func (m *MyInfo) MarshalNMDC() ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	buf.WriteString("$ALL ")

	name, err := m.Name.MarshalNMDC()
	if err != nil {
		return nil, err
	}
	buf.Write(name)

	buf.WriteString(" ")
	desc, err := m.Desc.MarshalNMDC()
	if err != nil {
		return nil, err
	}
	buf.Write(desc)
	buf.WriteString("<")
	buf.WriteString(m.Client)
	var a []string
	if m.Version != "" {
		buf.WriteString(" ")
		a = append(a, "V:"+m.Version)
	} else if m.Client != "" {
		buf.WriteString(",")
	}
	a = append(a, "M:"+string(m.Mode))
	var hubs []string
	for _, inf := range m.Hubs {
		hubs = append(hubs, strconv.Itoa(inf))
	}
	a = append(a, "H:"+strings.Join(hubs, "/"))
	a = append(a, "S:"+strconv.Itoa(m.Slots))
	for name, value := range m.Other {
		a = append(a, name+":"+value)
	}
	buf.WriteString(strings.Join(a, ","))
	buf.WriteString(">")
	buf.WriteString("$ $")
	buf.WriteString(m.Conn + string(m.Flag))
	buf.WriteString("$")
	buf.WriteString(m.Email)
	buf.WriteString("$")
	buf.WriteString(strconv.FormatUint(m.ShareSize, 10))
	buf.WriteString("$")
	return buf.Bytes(), nil
}

func (m *MyInfo) UnmarshalNMDC(data []byte) error {
	// $ALL johndoe <++ V:0.673,M:P,H:0/1/0,S:2>$ $LAN(T3)0x31$example@example.com$1234$
	if !bytes.HasPrefix(data, []byte("$ALL ")) {
		return errors.New("invalid info command: wrong prefix")
	}
	data = bytes.TrimPrefix(data, []byte("$ALL "))

	i := bytes.Index(data, []byte(" "))
	if i < 0 {
		return errors.New("invalid info command: no separators")
	}
	if err := m.Name.UnmarshalNMDC(data[:i]); err != nil {
		return err
	}
	data = data[i+1:]
	l := len(data)
	fields := bytes.SplitN(data[:l-1], []byte("$"), 6)
	if len(fields) != 5 {
		return errors.New("invalid info command")
	}
	for i, field := range fields {
		switch i {
		case 0:
			var desc []byte
			if i := bytes.Index(field, []byte("<")); i > 0 {
				desc = field[:i]
				tag := field[i+1:]
				if len(tag) == 0 || tag[len(tag)-1] != '>' {
					return errors.New("invalid info tag")
				}
				tag = tag[:len(tag)-1]
				var client []byte
				var tags [][]byte
				i = bytes.Index(tag, []byte(" V:"))
				if i < 0 {
					tags = bytes.Split(tag, []byte(","))
				} else {
					client = tag[:i]
					tags = bytes.Split(tag[i+1:], []byte(","))
				}
				other := make(map[string]string)
				for r, field := range tags {
					i = bytes.Index(field, []byte(":"))
					if i < 0 && r < 1 {
						client = field
					} else if i >= 0 {
						name := string(field[:i])
						value := string(field[i+1:])
						switch name {
						case "V":
							m.Version = value
						case "M":
							switch value {
							case "A":
								m.Mode = UserModeActive
							case "P":
								m.Mode = UserModePassive
							case "5":
								m.Mode = UserModeSOCKS5
							default:
								if len([]byte(value)) == 1 {
									m.Mode = UserMode(field[0])
								} else {
									return fmt.Errorf("invalid user mode")
								}
							}
						case "H":
							hubs := strings.Split(value, "/")
							if len(hubs) > 3 {
								return fmt.Errorf("hubs info contain: %v operators", len(hubs))
							}
							for i, inf := range hubs {
								h, err := strconv.Atoi(inf)
								if err != nil {
									return fmt.Errorf("invalid info hubs: %v", err)
								}
								m.Hubs[i] = h
							}
						case "S":
							slots, err := strconv.Atoi(value)
							if err != nil {
								return errors.New("invalid info slots")
							}
							m.Slots = int(slots)
						default:
							other[name] = value
						}
					} else {
						return fmt.Errorf("unknown field in tag: %q", field)
					}
				}
				m.Client = string(client)
				if len(other) != 0 {
					m.Other = other
				}
			} else {
				desc = field
			}
			if err := m.Desc.UnmarshalNMDC(desc); err != nil {
				return err
			}
		case 1:
			if len(field) > 1 || len(field) == 0 {
				return fmt.Errorf("invalid info command: unknown field %v", string(field))
			} else {
				if m.Mode == 0x0 {
					var mode UserMode
					switch string(field) {
					case "A":
						mode = UserModeActive
					case "P":
						mode = UserModePassive
					case "5":
						mode = UserModeSOCKS5
					default:
						if string(field) != " " {
							mode = UserMode(field[0])
						}
					}
					m.Mode = mode
				} else {
					if string(field) != " " {
						return fmt.Errorf("invalid info command: unknown field %v", string(field))
					}
				}
			}
		case 2:
			if len(field) == 0 {
				return errors.New("invalid info connection")
			}
			l := len(field)
			m.Flag = UserFlag(field[l-1])
			m.Conn = string(field[:l-1])
		case 3:
			m.Email = string(field)
		case 4:
			if s := string(field); s == "" {
				m.ShareSize = 0
			} else {
				size, err := strconv.ParseUint(s, 10, 64)
				if err != nil {
					return err
				}
				m.ShareSize = size
			}
		}
	}
	return nil
}

type OpList struct {
	List []Name
}

func (*OpList) Cmd() string {
	return "OpList"
}

func (m *OpList) MarshalNMDC() ([]byte, error) {
	if len(m.List) == 0 {
		return nil, nil
	}
	sub := make([][]byte, 0, len(m.List))
	for _, name := range m.List {
		data, err := name.MarshalNMDC()
		if err != nil {
			return nil, err
		}
		sub = append(sub, data)
	}
	return bytes.Join(sub, []byte("$$")), nil
}

func (m *OpList) UnmarshalNMDC(data []byte) error {
	if len(data) == 0 {
		m.List = nil
		return nil
	}
	// TODO: does it always contain a trailing '$$', or it's a single hub returning an empty name?
	data = bytes.TrimSuffix(data, []byte("$$"))
	sub := bytes.Split(data, []byte("$$"))
	m.List = make([]Name, 0, len(sub))
	for _, b := range sub {
		var name Name
		if err := name.UnmarshalNMDC(b); err != nil {
			return err
		}
		m.List = append(m.List, name)
	}
	return nil
}

type BotList struct {
	List []Name
}

func (*BotList) Cmd() string {
	return "BotList"
}

func (m *BotList) MarshalNMDC() ([]byte, error) {
	if len(m.List) == 0 {
		return nil, nil
	}
	return ((*OpList)(m)).MarshalNMDC()
}

func (m *BotList) UnmarshalNMDC(data []byte) error {
	if len(data) == 0 {
		m.List = nil
		return nil
	}
	return ((*OpList)(m)).UnmarshalNMDC(data)
}

type ConnectToMe struct {
	Targ    Name
	Address string
	Secure  bool
}

func (m *ConnectToMe) Cmd() string {
	return "ConnectToMe"
}

func (m *ConnectToMe) MarshalNMDC() ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	data, err := m.Targ.MarshalNMDC()
	if err != nil {
		return nil, err
	}
	buf.Write(data)
	buf.WriteByte(' ')
	buf.WriteString(m.Address)
	if m.Secure {
		buf.WriteByte('S')
	}
	return buf.Bytes(), nil
}

func (m *ConnectToMe) UnmarshalNMDC(data []byte) error {
	i := bytes.Index(data, []byte(" "))
	if i < 0 {
		return errors.New("invalid ConnectToMe command")
	}
	if err := m.Targ.UnmarshalNMDC(data[:i]); err != nil {
		return err
	}
	addr := data[i+1:]
	if l := len(addr); l != 0 && addr[l-1] == 'S' {
		addr = addr[:l-1]
		m.Secure = true
	}
	m.Address = string(addr)
	return nil
}

type RevConnectToMe struct {
	From, To Name
}

func (m *RevConnectToMe) Cmd() string {
	return "RevConnectToMe"
}

func (m *RevConnectToMe) MarshalNMDC() ([]byte, error) {
	from, err := m.From.MarshalNMDC()
	if err != nil {
		return nil, err
	}
	to, err := m.To.MarshalNMDC()
	if err != nil {
		return nil, err
	}
	return bytes.Join([][]byte{from, to}, []byte(" ")), nil
}

func (m *RevConnectToMe) UnmarshalNMDC(data []byte) error {
	i := bytes.Index(data, []byte(" "))
	if i < 0 {
		return errors.New("invalid RevConnectToMe command")
	}
	if err := m.From.UnmarshalNMDC(data[:i]); err != nil {
		return err
	}
	if err := m.To.UnmarshalNMDC(data[i+1:]); err != nil {
		return err
	}
	return nil
}

type PrivateMessage struct {
	To, From Name
	Text     String
}

func (m *PrivateMessage) Cmd() string {
	return "To:"
}

func (m *PrivateMessage) MarshalNMDC() ([]byte, error) {
	to, err := m.To.MarshalNMDC()
	if err != nil {
		return nil, err
	}
	buf := bytes.NewBuffer(nil)
	buf.Write(to)
	from, err := m.From.MarshalNMDC()
	if err != nil {
		return nil, err
	}
	buf.WriteString(" From: ")
	buf.Write(from)
	buf.WriteString(" $<")
	buf.Write(from)
	buf.WriteString("> ")
	text, err := m.Text.MarshalNMDC()
	if err != nil {
		return nil, err
	}
	buf.Write(text)
	return buf.Bytes(), nil
}

func (m *PrivateMessage) UnmarshalNMDC(data []byte) error {
	i := bytes.Index(data, []byte(" "))
	if i < 0 {
		return errors.New("invalid PrivateMessage")
	}
	if err := m.To.UnmarshalNMDC(data[:i]); err != nil {
		return err
	}
	data = data[i+1:]
	if !bytes.HasPrefix(data, []byte("From: ")) {
		return errors.New("invalid PrivateMessage")
	}
	data = bytes.TrimPrefix(data, []byte("From: "))
	i = bytes.Index(data, []byte(" "))
	if i < 0 {
		return errors.New("invalid PrivateMessage")
	}
	from := data[:i]
	data = data[i+1:]
	if !bytes.HasPrefix(data, []byte("$<")) {
		return errors.New("invalid PrivateMessage")
	}
	data = bytes.TrimPrefix(data, []byte("$<"))
	i = bytes.Index(data, []byte("> "))
	if !bytes.Equal(from, data[:i]) {
		return errors.New("invalid PrivateMessage")
	}
	if err := m.From.UnmarshalNMDC(from); err != nil {
		return err
	}
	text := data[i+2:]
	if err := m.Text.UnmarshalNMDC(text); err != nil {
		return err
	}
	return nil
}

type Failed struct {
	Text String
}

func (f *Failed) Cmd() string {
	return "Failed"
}

func (f *Failed) MarshalNMDC() ([]byte, error) {
	if f.Text == "" {
		return nil, nil
	}
	text, err := f.Text.MarshalNMDC()
	if err != nil {
		return nil, err
	}
	return []byte(text), nil
}

func (f *Failed) UnmarshalNMDC(text []byte) error {
	if err := f.Text.UnmarshalNMDC(text); err != nil {
		return err
	}
	return nil
}

type Error struct {
	Text String
}

func (e *Error) Cmd() string {
	return "Error"
}

func (e *Error) MarshalNMDC() ([]byte, error) {
	if e.Text == "" {
		return nil, nil
	}
	text, err := e.Text.MarshalNMDC()
	if err != nil {
		return nil, err
	}
	return []byte(text), nil
}

func (e *Error) UnmarshalNMDC(text []byte) error {
	if err := e.Text.UnmarshalNMDC(text); err != nil {
		return err
	}
	return nil
}

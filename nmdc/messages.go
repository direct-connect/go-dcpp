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

func UnmarshalMessage(name string, data []byte) (Message, error) {
	rt, ok := messages[name]
	if !ok {
		return &RawCommand{Name: name, Data: data}, nil
	}
	m := reflect.New(rt).Interface().(Message)
	if err := m.UnmarshalNMDC(data); err != nil {
		return nil, err
	}
	return m, nil
}

type ChatMessage struct {
	Name Name
	Text string
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
	// TODO: convert to connection encoding
	buf.WriteString(m.Text)
	buf.WriteByte('|')
	return buf.Bytes(), nil
}

func (m *ChatMessage) UnmarshalNMDC(data []byte) error {
	panic("special case")
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

type Hello struct {
	Name Name
}

func (*Hello) Cmd() string {
	return "Hello"
}

func (m *Hello) MarshalNMDC() ([]byte, error) {
	return m.Name.MarshalNMDC()
}

func (m *Hello) UnmarshalNMDC(data []byte) error {
	return m.Name.UnmarshalNMDC(data)
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

type HubName struct {
	Name Name
}

func (*HubName) Cmd() string {
	return "HubName"
}

func (m *HubName) MarshalNMDC() ([]byte, error) {
	return m.Name.MarshalNMDC()
}

func (m *HubName) UnmarshalNMDC(data []byte) error {
	return m.Name.UnmarshalNMDC(data)
}

type HubTopic struct {
	Text string
}

func (*HubTopic) Cmd() string {
	return "HubTopic"
}

func (m *HubTopic) MarshalNMDC() ([]byte, error) {
	return []byte(m.Text), nil // TODO: encoding
}

func (m *HubTopic) UnmarshalNMDC(data []byte) error {
	m.Text = string(data) // TODO: encoding
	return nil
}

type MyNick struct {
	Name Name
}

func (*MyNick) Cmd() string {
	return "MyNick"
}

func (m *MyNick) MarshalNMDC() ([]byte, error) {
	return m.Name.MarshalNMDC()
}

func (m *MyNick) UnmarshalNMDC(data []byte) error {
	return m.Name.UnmarshalNMDC(data)
}

type ValidateNick struct {
	Name Name
}

func (*ValidateNick) Cmd() string {
	return "ValidateNick"
}

func (m *ValidateNick) MarshalNMDC() ([]byte, error) {
	return m.Name.MarshalNMDC()
}

func (m *ValidateNick) UnmarshalNMDC(data []byte) error {
	return m.Name.UnmarshalNMDC(data)
}

type Quit struct {
	Name Name
}

func (*Quit) Cmd() string {
	return "Quit"
}

func (m *Quit) MarshalNMDC() ([]byte, error) {
	return m.Name.MarshalNMDC()
}

func (m *Quit) UnmarshalNMDC(data []byte) error {
	return m.Name.UnmarshalNMDC(data)
}

type Lock struct {
	Lock string
	PK   string
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
	Desc      string
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
	buf.WriteString(m.Desc)
	buf.WriteString("<")
	buf.WriteString(m.Client)
	buf.WriteString(" ")
	var a []string
	a = append(a, "V:"+m.Version)
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
		return errors.New("invalid info command")
	}
	data = bytes.TrimPrefix(data, []byte("$ALL "))

	i := bytes.Index(data, []byte(" "))
	if i < 0 {
		return errors.New("invalid info command")
	}
	if err := m.Name.UnmarshalNMDC(data[:i]); err != nil {
		return err
	}
	data = data[i+1:]

	fields := bytes.SplitN(data, []byte("$ $"), 2)
	if len(fields) != 2 {
		return errors.New("invalid info command")
	}
	desc := fields[0]
	data = fields[1]
	if i := bytes.Index(desc, []byte("<")); i >= 0 {
		tag := desc[i+1:]
		desc = desc[:i]
		if len(tag) == 0 || tag[len(tag)-1] != '>' {
			return errors.New("invalid info tag")
		}
		i = bytes.Index(tag, []byte(" "))
		if i < 0 {
			return errors.New("invalid client indicate")
		}
		m.Client = string(tag[:i])
		tag = tag[i+1 : len(tag)-1]
		fields = bytes.Split(tag, []byte(","))
		other := make(map[string]string)
		for _, field := range fields {
			i = bytes.Index(field, []byte(":"))
			if i < 0 {
				return errors.New("unknown field in tag")
			}
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
					if len([]byte(value)) == 0 {
						m.Mode = UserMode(' ')
					} else if len([]byte(value)) == 1 {
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
		}
		m.Other = other
	}
	m.Desc = string(desc)
	l := len(data)
	if l == 0 || data[l-1] != '$' {
		return errors.New("invalid info connection")
	}
	fields = bytes.SplitN(data[:l-1], []byte("$"), 3)
	if len(fields) != 3 {
		return errors.New("invalid info connection")
	}
	for i, field := range fields {
		switch i {
		case 0:
			if len(field) == 0 {
				return errors.New("invalid info connection")
			}
			l := len(field)
			m.Flag = UserFlag(field[l-1])
			m.Conn = string(field[:l-1])
		case 1:
			m.Email = string(field)
		case 2:
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

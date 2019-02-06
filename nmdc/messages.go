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
	RegisterMessage(&BotINFO{})
	RegisterMessage(&Lock{})
	RegisterMessage(&Key{})
	RegisterMessage(&Supports{})
	RegisterMessage(&GetNickList{})
	RegisterMessage(&HubINFO{})
	RegisterMessage(&MyInfo{})
	RegisterMessage(&OpList{})
	RegisterMessage(&BotList{})
	RegisterMessage(&UserIP{})
	RegisterMessage(&ConnectToMe{})
	RegisterMessage(&RevConnectToMe{})
	RegisterMessage(&PrivateMessage{})
	RegisterMessage(&Failed{})
	RegisterMessage(&Error{})
	RegisterMessage(&Search{})
	RegisterMessage(&FailOver{})
	RegisterMessage(&MCTo{})
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

func Marshal(m Message) ([]byte, error) {
	if cm, ok := m.(*ChatMessage); ok {
		// special case
		return cm.MarshalNMDC()
	}
	data, err := m.MarshalNMDC()
	if err != nil {
		return nil, err
	}
	name := m.Cmd()
	n := 1 + len(name) + 1
	if len(data) != 0 {
		n += 1 + len(data)
	}
	buf := make([]byte, n)
	i := 0
	buf[i] = '$'
	i++
	i += copy(buf[i:], name)
	if len(data) != 0 {
		buf[i] = ' '
		i++
		i += copy(buf[i:], data)
	}
	buf[i] = '|'
	return buf, nil
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

func (m *ChatMessage) String() string {
	if m.Name == "" {
		return string(m.Text)
	}
	return "<" + string(m.Name) + "> " + string(m.Text)
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

type BotINFO struct {
	Name
}

func (*BotINFO) Cmd() string {
	return "BotINFO"
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
	Ref  string
}

func (*Lock) Cmd() string {
	return "Lock"
}

func (m *Lock) MarshalNMDC() ([]byte, error) {
	lock := []string{m.Lock}
	if len(m.PK) != 0 {
		lock = append(lock, " Pk=", m.PK)
	} else {
		lock = append(lock, " ")
	}
	if len(m.Ref) != 0 {
		lock = append(lock, "Ref=", m.Ref)
	}
	return []byte(strings.Join(lock, "")), nil
}

func (m *Lock) UnmarshalNMDC(data []byte) error {
	i := bytes.Index(data, []byte(" "))
	if i >= 0 {
		m.Lock = string(data[:i])
	}
	data = data[i+1:]
	if bytes.HasPrefix(data, []byte("Pk=")) {
		data = bytes.TrimPrefix(data, []byte("Pk="))
	}
	i = bytes.Index(data, []byte("Ref="))
	if i >= 0 {
		m.PK = string(data[:i])
		m.Ref = string(data[i+4:])
	} else {
		m.PK = string(data)
	}
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

type HubINFO struct {
	Name     Name
	Host     string
	Desc     String
	I1       int
	I2       int
	I3       int
	I4       int
	Soft     string
	Owner    string
	State    String
	Encoding string
}

func (*HubINFO) Cmd() string {
	return "HubINFO"
}

func (m *HubINFO) MarshalNMDC() ([]byte, error) {
	var a [][]byte
	name, err := m.Name.MarshalNMDC()
	if err != nil {
		return nil, err
	}
	a = append(a, name)
	a = append(a, []byte(m.Host))
	desc, err := m.Desc.MarshalNMDC()
	if err != nil {
		return nil, err
	}
	a = append(a, desc)
	a = append(a, []byte(strconv.Itoa(m.I1)))
	a = append(a, []byte(strconv.Itoa(m.I2)))
	a = append(a, []byte(strconv.Itoa(m.I3)))
	a = append(a, []byte(strconv.Itoa(m.I4)))
	a = append(a, []byte(m.Soft))
	a = append(a, []byte(m.Owner))
	state, err := m.State.MarshalNMDC()
	if err != nil {
		return nil, err
	}
	a = append(a, state)
	a = append(a, []byte(m.Encoding))
	buf := bytes.NewBuffer(bytes.Join(a, []byte("$")))
	return buf.Bytes(), nil
}

func (m *HubINFO) UnmarshalNMDC(data []byte) error {
	fields := bytes.SplitN(data, []byte("$"), 12)
	if len(fields) > 11 {
		return fmt.Errorf("hub info contain: %v parameters", len(fields))
	}
	for i, field := range fields {
		switch i {
		case 0:
			if err := m.Name.UnmarshalNMDC(field); err != nil {
				return err
			}
		case 1:
			m.Host = string(field)
		case 2:
			if err := m.Desc.UnmarshalNMDC(field); err != nil {
				return err
			}
		case 3:
			i1, err := strconv.Atoi(strings.TrimSpace(string(field)))
			if err != nil {
				return errors.New("invalid i1")
			}
			m.I1 = int(i1)
		case 4:
			i2, err := strconv.Atoi(strings.TrimSpace(string(field)))
			if err != nil {
				return errors.New("invalid i2")
			}
			m.I2 = int(i2)
		case 5:
			i3, err := strconv.Atoi(strings.TrimSpace(string(field)))
			if err != nil {
				return errors.New("invalid i3")
			}
			m.I3 = int(i3)
		case 6:
			i4, err := strconv.Atoi(strings.TrimSpace(string(field)))
			if err != nil {
				return errors.New("invalid i4")
			}
			m.I4 = int(i4)
		case 7:
			m.Soft = string(field)
		case 8:
			m.Owner = string(field)
		case 9:
			if err := m.State.UnmarshalNMDC(field); err != nil {
				return err
			}
		case 10:
			m.Encoding = string(field)
		}
	}
	return nil
}

type UserMode byte

const (
	UserModeUnknown = UserMode(0x00)
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
	Name           Name
	Desc           String
	Client         string
	Version        string
	Mode           UserMode
	HubsNormal     int
	HubsRegistered int
	HubsOperator   int
	Slots          int
	Other          map[string]string
	Conn           string
	Flag           UserFlag
	Email          string
	ShareSize      uint64
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
	buf.WriteString(" ")
	var a []string
	a = append(a, "V:"+m.Version)
	if m.Mode != UserModeUnknown {
		a = append(a, "M:"+string(m.Mode))
	} else {
		a = append(a, "M:")
	}
	hubs := []string{
		strconv.Itoa(m.HubsNormal),
		strconv.Itoa(m.HubsRegistered),
		strconv.Itoa(m.HubsOperator),
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
			m.Mode = UserModeUnknown
			i = bytes.Index(field, []byte("<"))
			if i < 0 {
				desc = field
			} else {
				desc = field[:i]
				tag := field[i+1:]
				if len(tag) == 0 {
					return errors.New("invalid info tag")
				}
				if tag[len(tag)-1] == '>' {
					tag = tag[:len(tag)-1]
				}
				if err := m.unmarshalTag(tag); err != nil {
					return err
				}
			}
			if err := m.Desc.UnmarshalNMDC(desc); err != nil {
				return err
			}
		case 1:
			if string(field) != " " {
				return fmt.Errorf("unknown field before connection %v", string(field))
			}
		case 2:
			if len(field) > 0 {
				l := len(field)
				m.Flag = UserFlag(field[l-1])
				m.Conn = string(field[:l-1])
			}
		case 3:
			m.Email = string(field)
		case 4:
			if len(field) == 0 {
				continue
			}
			size, err := strconv.ParseUint(strings.TrimSpace(string(field)), 10, 64)
			if err != nil {
				return err
			}
			m.ShareSize = size
		}
	}
	return nil
}

func (m *MyInfo) unmarshalTag(tag []byte) error {
	var (
		client []byte
		tags   [][]byte
	)
	i := bytes.Index(tag, []byte(" V:"))
	if i < 0 {
		i = bytes.Index(tag, []byte(" v:"))
	}
	if i < 0 {
		tags = bytes.Split(tag, []byte(","))
	} else {
		client = tag[:i]
		tags = bytes.Split(tag[i+1:], []byte(","))
	}
	other := make(map[string]string)
	for r, field := range tags {
		if len(field) == 0 {
			continue
		}
		i = bytes.Index(field, []byte(":"))
		if i < 0 && r < 1 {
			client = field
			continue
		}
		if i < 0 {
			return fmt.Errorf("unknown field in tag: %q", field)
		}
		name := string(field[:i])
		value := string(field[i+1:])
		switch name {
		case "V", "v":
			m.Version = value
		case "M", "m":
			if len([]byte(value)) == 1 {
				m.Mode = UserMode(value[0])
				continue
			}
			m.Mode = UserModeUnknown
		case "H", "h":
			hubs := strings.Split(value, "/")
			if len(hubs) > 3 {
				return fmt.Errorf("hubs info contain: %v operators", len(hubs))
			}
			normal, err := strconv.Atoi(strings.TrimSpace(hubs[0]))
			if err != nil {
				return fmt.Errorf("invalid info hubs normal: %v", err)
			}
			m.HubsNormal = normal
			registered, err := strconv.Atoi(strings.TrimSpace(hubs[1]))
			if err != nil {
				return fmt.Errorf("invalid info hubs registered: %v", err)
			}
			m.HubsRegistered = registered
			operator, err := strconv.Atoi(strings.TrimSpace(hubs[2]))
			if err != nil {
				return fmt.Errorf("invalid info hubs operator: %v", err)
			}
			m.HubsOperator = operator
		case "S", "s":
			slots, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return errors.New("invalid info slots")
			}
			m.Slots = int(slots)
		default:
			other[name] = value
		}
	}
	m.Client = string(client)
	if len(other) != 0 {
		m.Other = other
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

type UserIP struct {
	Name Name
	IP   string
}

func (*UserIP) Cmd() string {
	return "UserIP"
}

func (m *UserIP) MarshalNMDC() ([]byte, error) {
	name, err := m.Name.MarshalNMDC()
	if err != nil {
		return nil, err
	}
	if len(m.IP) == 0 {
		return name, nil
	}
	return bytes.Join([][]byte{name, []byte(m.IP)}, []byte(" ")), nil
}

func (m *UserIP) UnmarshalNMDC(data []byte) error {
	i := bytes.Index(data, []byte(" "))
	if i >= 0 {
		m.IP = string(data[i+1:])
		data = data[:i]
	}
	if err := m.Name.UnmarshalNMDC(data); err != nil {
		return err
	}
	return nil
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

func (m *Failed) Cmd() string {
	return "Failed"
}

func (m *Failed) MarshalNMDC() ([]byte, error) {
	if m.Text == "" {
		return nil, nil
	}
	text, err := m.Text.MarshalNMDC()
	if err != nil {
		return nil, err
	}
	return []byte(text), nil
}

func (m *Failed) UnmarshalNMDC(text []byte) error {
	if err := m.Text.UnmarshalNMDC(text); err != nil {
		return err
	}
	return nil
}

type Error struct {
	Text String
}

func (m *Error) Cmd() string {
	return "Error"
}

func (m *Error) MarshalNMDC() ([]byte, error) {
	if m.Text == "" {
		return nil, nil
	}
	text, err := m.Text.MarshalNMDC()
	if err != nil {
		return nil, err
	}
	return []byte(text), nil
}

func (m *Error) UnmarshalNMDC(text []byte) error {
	if err := m.Text.UnmarshalNMDC(text); err != nil {
		return err
	}
	return nil
}

type DataType byte

const (
	DataTypeAnyFileType     = DataType('1')
	DataTypeAudioFiles      = DataType('2')
	DataTypeCompressedFiles = DataType('3')
	DataTypeDocumentFiles   = DataType('4')
	DataTypeExecutableFiles = DataType('5')
	DataTypePictureFiles    = DataType('6')
	DataTypeVideoFiles      = DataType('7')
	DataTypeFolders         = DataType('8')
	DataTypeTTH             = DataType('9')
)

type Search struct {
	Address        string
	Nick           Name
	SizeRestricted bool
	IsMaxSize      bool
	Size           uint64
	DataType       DataType
	Pattern        string
}

func (*Search) Cmd() string {
	return "Search"
}

func (m *Search) MarshalNMDC() ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	if m.Address != "" {
		buf.Write([]byte(m.Address))
	} else {
		nick, err := m.Nick.MarshalNMDC()
		if err != nil {
			return nil, err
		}
		buf.Write([]byte("Hub:"))
		buf.Write(nick)
	}
	buf.WriteByte(' ')
	var a [][]byte
	if m.SizeRestricted == true {
		a = append(a, []byte{'T'})
	} else {
		a = append(a, []byte{'F'})
	}
	if m.IsMaxSize == true {
		a = append(a, []byte{'T'})
	} else {
		a = append(a, []byte{'F'})
	}
	a = append(a, []byte(strconv.FormatUint(m.Size, 10)))
	a = append(a, []byte{byte(m.DataType)})
	str := strings.Replace(m.Pattern, " ", "$", len(m.Pattern))
	pattern := []byte(str)
	a = append(a, []byte(String(pattern)))
	buf.Write(bytes.Join(a, []byte("?")))
	return buf.Bytes(), nil
}

func (m *Search) UnmarshalNMDC(data []byte) error {
	fields := bytes.SplitN(data, []byte(" "), 3)
	if len(fields) != 2 {
		return errors.New("invalid search command")
	}
	if bytes.HasPrefix(fields[0], []byte("Hub:")) {
		field := bytes.TrimPrefix(fields[0], []byte("Hub:"))
		err := m.Nick.UnmarshalNMDC(field)
		if err != nil {
			return err
		}
	} else {
		m.Address = string(fields[0])
	}
	err := m.unmarshalString(fields[1])
	if err != nil {
		return err
	}
	return nil
}

func (m *Search) unmarshalString(data []byte) error {
	fields := bytes.SplitN(data, []byte("?"), 6)
	if len(fields) != 5 {
		return errors.New("invalid search string")
	}
	for i, field := range fields {
		switch i {
		case 0:
			flag, err := unmarshalBoolFlag(field)
			if err != nil {
				return err
			}
			m.SizeRestricted = flag
		case 1:
			flag, err := unmarshalBoolFlag(field)
			if err != nil {
				return err
			}
			m.IsMaxSize = flag
		case 2:
			if len(field) == 0 {
				continue
			}
			size, err := strconv.ParseUint(strings.TrimSpace(string(field)), 10, 64)
			if err != nil {
				return err
			}
			m.Size = size
		case 3:
			if len([]byte(field)) != 1 {
				return fmt.Errorf("invalid data type")
			}
			m.DataType = DataType(field[0])
		case 4:
			var str String
			err := str.UnmarshalNMDC(field)
			if err != nil {
				return err
			}
			m.Pattern = strings.Replace(string(str), "$", " ", len(str))
		}
	}
	return nil
}

func unmarshalBoolFlag(data []byte) (bool, error) {
	if len([]byte(data)) != 1 {
		return false, fmt.Errorf("invalid bool flag")
	}
	if data[0] == 'T' {
		return true, nil
	}
	if data[0] == 'F' {
		return false, nil
	}
	return false, fmt.Errorf("invalid bool flag")
}

type FailOver struct {
	Host []string
}

func (*FailOver) Cmd() string {
	return "FailOver"
}

func (m *FailOver) MarshalNMDC() ([]byte, error) {
	return []byte(strings.Join(m.Host, ",")), nil
}

func (m *FailOver) UnmarshalNMDC(data []byte) error {
	hosts := bytes.Split(data, []byte(","))
	for _, host := range hosts {
		m.Host = append(m.Host, string(host))
	}
	return nil
}

type MCTo struct {
	Target, Sender Name
	Text           String
}

func (*MCTo) Cmd() string {
	return "MCTo"
}

func (m *MCTo) MarshalNMDC() ([]byte, error) {
	target, err := m.Target.MarshalNMDC()
	if err != nil {
		return nil, err
	}
	buf := bytes.NewBuffer(nil)
	buf.Write(target)
	sender, err := m.Sender.MarshalNMDC()
	if err != nil {
		return nil, err
	}
	buf.WriteString(" $")
	buf.Write(sender)
	text, err := m.Text.MarshalNMDC()
	if err != nil {
		return nil, err
	}
	buf.WriteString(" ")
	buf.Write(text)
	return buf.Bytes(), nil
}

func (m *MCTo) UnmarshalNMDC(data []byte) error {
	i := bytes.Index(data, []byte(" $"))
	if i < 0 {
		return errors.New("invalid PrivateMessage")
	}
	if err := m.Target.UnmarshalNMDC(data[:i]); err != nil {
		return err
	}
	data = data[i+2:]
	i = bytes.Index(data, []byte(" "))
	if i < 0 {
		return errors.New("invalid PrivateMessage")
	}
	if err := m.Sender.UnmarshalNMDC(data[:i]); err != nil {
		return err
	}
	if err := m.Text.UnmarshalNMDC(data[i+1:]); err != nil {
		return err
	}
	return nil
}

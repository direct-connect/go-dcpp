package adc

import (
	"bytes"
	"encoding/base32"
	"fmt"
	"os"
	"reflect"
	"strconv"

	"github.com/direct-connect/go-dcpp/tiger"
)

var (
	messages = make(map[MsgType]reflect.Type)
)

func init() {
	RegisterMessage(Supported{})
	RegisterMessage(Status{})
	RegisterMessage(SIDAssign{})
	RegisterMessage(User{})
	RegisterMessage(GetPassword{})
	RegisterMessage(Password{})
	RegisterMessage(RevConnectRequest{})
	RegisterMessage(ConnectRequest{})
	RegisterMessage(GetInfoRequest{})
	RegisterMessage(GetRequest{})
	RegisterMessage(GetResponse{})
	RegisterMessage(SearchRequest{})
	RegisterMessage(SearchResult{})
	RegisterMessage(ChatMessage{})
	RegisterMessage(Disconnect{})
}

type Message interface {
	Cmd() MsgType
}

func RegisterMessage(m Message) {
	name := m.Cmd()
	if _, ok := messages[name]; ok {
		panic(fmt.Errorf("%q already registered", name.String()))
	}
	rt := reflect.TypeOf(m)
	if rt.Kind() == reflect.Ptr {
		rt = rt.Elem()
	}
	messages[name] = rt
}

func UnmarshalMessage(name MsgType, data []byte) (Message, error) {
	rt, ok := messages[name]
	if !ok {
		return RawMessage{Type: name, Data: data}, nil
	}
	rv := reflect.New(rt)
	if err := Unmarshal(data, rv.Interface()); err != nil {
		return nil, err
	}
	return rv.Elem().Interface().(Message), nil
}

var (
	_ Message     = RawMessage{}
	_ Marshaler   = RawMessage{}
	_ Unmarshaler = (*RawMessage)(nil)
)

type RawMessage struct {
	Type MsgType
	Data []byte
}

func (m RawMessage) Cmd() MsgType {
	return m.Type
}

func (m RawMessage) MarshalAdc() ([]byte, error) {
	return m.Data, nil
}

func (m *RawMessage) UnmarshalAdc(data []byte) error {
	m.Data = data // TODO: copy?
	return nil
}

var (
	_ Message     = Supported{}
	_ Marshaler   = Supported{}
	_ Unmarshaler = (*Supported)(nil)
)

type Supported struct {
	Features ModFeatures
}

func (Supported) Cmd() MsgType {
	return MsgType{'S', 'U', 'P'}
}

func (m Supported) MarshalAdc() ([]byte, error) {
	return m.Features.MarshalAdc()
}

func (m *Supported) UnmarshalAdc(data []byte) error {
	return m.Features.UnmarshalAdc(data)
}

type Severity int

const (
	Success     = Severity(0)
	Recoverable = Severity(1)
	Fatal       = Severity(2)
)

var (
	_ Message     = Status{}
	_ Marshaler   = Status{}
	_ Unmarshaler = (*Status)(nil)
)

type Status struct {
	Sev  Severity
	Code int
	Msg  string
}

func (Status) Cmd() MsgType {
	return MsgType{'S', 'T', 'A'}
}

func (st Status) Ok() bool {
	return st.Sev == Success
}
func (st Status) Recoverable() bool {
	return st.Ok() || st.Sev == Recoverable
}
func (st Status) Err() error {
	if !st.Ok() {
		if st.Code == 51 {
			return os.ErrNotExist
		}
		return Error{st}
	}
	return nil
}
func (st *Status) UnmarshalAdc(s []byte) error {
	sub := bytes.SplitN(s, []byte(" "), 2)
	code, err := strconv.Atoi(string(sub[0]))
	if err != nil {
		return fmt.Errorf("wrong status code: %v", err)
	}
	st.Code = code % 100
	st.Sev = Severity(code / 100)
	st.Msg = ""
	if len(sub) > 1 {
		st.Msg = unescape(sub[1])
	}
	return nil
}
func (st Status) MarshalAdc() ([]byte, error) {
	s := fmt.Sprintf("%d%02d %s", int(st.Sev), st.Code, escape(st.Msg))
	return []byte(s), nil
}

var (
	_ Message     = SIDAssign{}
	_ Marshaler   = SIDAssign{}
	_ Unmarshaler = (*SIDAssign)(nil)
)

type SIDAssign struct {
	SID SID
}

func (SIDAssign) Cmd() MsgType {
	return MsgType{'S', 'I', 'D'}
}

func (m SIDAssign) MarshalAdc() ([]byte, error) {
	return m.SID.MarshalAdc()
}

func (m *SIDAssign) UnmarshalAdc(data []byte) error {
	return m.SID.UnmarshalAdc(data)
}

var _ Message = User{}

type User struct {
	Id   CID    `adc:"ID"`
	Pid  *PID   `adc:"PD"` // sent only to hub
	Name string `adc:"NI,req"`

	Ip4  string `adc:"I4"`
	Ip6  string `adc:"I6"`
	Udp4 int    `adc:"U4"`
	Udp6 int    `adc:"U6"`

	ShareSize  int64 `adc:"SS,req"`
	ShareFiles int   `adc:"SF,req"`

	Version     string `adc:"VE,req"`
	Application string `adc:"AP"`

	MaxUpload   string `adc:"US"`
	MaxDownload string `adc:"DS"`

	Slots         int `adc:"SL,req"`
	SlotsFree     int `adc:"FS,req"`
	AutoSlotLimit int `adc:"AS"`

	Email string `adc:"EM"`
	Desc  string `adc:"DE"`

	HubsNormal     int `adc:"HN,req"`
	HubsRegistered int `adc:"HR,req"`
	HubsOperator   int `adc:"HO,req"`

	Token string `adc:"TO"` // C-C only

	Type UserType `adc:"CT"`
	Away AwayType `adc:"AW"`
	Ref  string   `adc:"RF"`

	Features ExtFeatures `adc:"SU,req"`

	KP string `adc:"KP"`
}

func (User) Cmd() MsgType {
	return MsgType{'I', 'N', 'F'}
}

var (
	_ Message     = UserMod{}
	_ Marshaler   = UserMod{}
	_ Unmarshaler = (*UserMod)(nil)
)

type UserMod map[[2]byte]string

func (m *UserMod) UnmarshalAdc(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if *m == nil {
		*m = make(map[[2]byte]string)
	}
	mp := *m
	sub := bytes.Split(data, []byte(" "))
	for _, v := range sub {
		if len(v) < 2 {
			return fmt.Errorf("invalid field: %q", string(v))
		}
		k := [2]byte{v[0], v[1]}
		v = v[2:]
		mp[k] = string(v)
	}
	return nil
}

func (m UserMod) MarshalAdc() ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	for k, v := range m {
		if buf.Len() != 0 {
			buf.WriteString(" ")
		}
		buf.Write(k[:])
		data, err := String(v).MarshalAdc()
		if err != nil {
			return nil, err
		}
		buf.Write(data)
	}
	return buf.Bytes(), nil
}

func (UserMod) Cmd() MsgType {
	return MsgType{'I', 'N', 'F'}
}

var base32Enc = base32.StdEncoding.WithPadding(base32.NoPadding)

var (
	_ Message     = GetPassword{}
	_ Marshaler   = GetPassword{}
	_ Unmarshaler = (*GetPassword)(nil)
)

type GetPassword struct {
	Salt []byte
}

func (GetPassword) Cmd() MsgType {
	return MsgType{'G', 'P', 'A'}
}

func (m GetPassword) MarshalAdc() ([]byte, error) {
	size := base32Enc.EncodedLen(len(m.Salt))
	data := make([]byte, size)
	base32Enc.Encode(data, m.Salt)
	return data, nil
}

func (m *GetPassword) UnmarshalAdc(data []byte) error {
	size := base32Enc.DecodedLen(len(data))
	m.Salt = make([]byte, size)
	_, err := base32Enc.Decode(m.Salt, data)
	return err
}

var _ Message = Password{}

type Password struct {
	Hash tiger.Hash `adc:"#"`
}

func (Password) Cmd() MsgType {
	return MsgType{'P', 'A', 'S'}
}

var _ Message = RevConnectRequest{}

type RevConnectRequest struct {
	Proto string `adc:"#"`
	Token string `adc:"#"`
}

func (RevConnectRequest) Cmd() MsgType {
	return MsgType{'R', 'C', 'M'}
}

var _ Message = ConnectRequest{}

type ConnectRequest struct {
	Proto string `adc:"#"`
	Port  int    `adc:"#"`
	Token string `adc:"#"`
}

func (ConnectRequest) Cmd() MsgType {
	return MsgType{'C', 'T', 'M'}
}

var _ Message = GetInfoRequest{}

type GetInfoRequest struct {
	Type string `adc:"#"`
	Path string `adc:"#"`
}

func (GetInfoRequest) Cmd() MsgType {
	return MsgType{'G', 'F', 'I'}
}

var _ Message = GetRequest{}

type GetRequest struct {
	Type  string `adc:"#"`
	Path  string `adc:"#"`
	Start int64  `adc:"#"`
	Bytes int64  `adc:"#"`
}

func (GetRequest) Cmd() MsgType {
	return MsgType{'G', 'E', 'T'}
}

var _ Message = GetResponse{}

type GetResponse GetRequest

func (GetResponse) Cmd() MsgType {
	return MsgType{'S', 'N', 'D'}
}

type FileType int

const (
	FileTypeAny  FileType = 0
	FileTypeFile FileType = 1
	FileTypeDir  FileType = 2
)

var _ Message = SearchRequest{}

type SearchRequest struct {
	Token string   `adc:"TO"`
	And   []string `adc:"AN"`
	Not   []string `adc:"NO"`
	Ext   []string `adc:"EX"`

	Le int64 `adc:"LE"`
	Ge int64 `adc:"GE"`
	Eq int64 `adc:"EQ"`

	Type FileType `adc:"TY"`

	// TIGR ext
	Tiger string `adc:"TR"`

	// SEGA ext
	Group ExtGroup `adc:"GR"`
	NoExt []string `adc:"RX"`
}

func (SearchRequest) Cmd() MsgType {
	return MsgType{'S', 'C', 'H'}
}

var _ Message = SearchResult{}

type SearchResult struct {
	Token string `adc:"TO"`
	Path  string `adc:"FN"`
	Size  int64  `adc:"SI"`
	Slots int    `adc:"SL"`

	// TIGR ext
	Tiger TTH `adc:"TR"`
}

func (SearchResult) Cmd() MsgType {
	return MsgType{'R', 'E', 'S'}
}

var (
	_ Message = ChatMessage{}
)

type ChatMessage struct {
	Text String `adc:"#"`
	PM   *SID   `adc:"PM"`
	// TODO: TS
}

func (ChatMessage) Cmd() MsgType {
	return MsgType{'M', 'S', 'G'}
}

var (
	_ Message     = Disconnect{}
	_ Marshaler   = Disconnect{}
	_ Unmarshaler = (*Disconnect)(nil)
)

type Disconnect struct {
	ID SID
}

func (Disconnect) Cmd() MsgType {
	return MsgType{'Q', 'U', 'I'}
}

func (m Disconnect) MarshalAdc() ([]byte, error) {
	return m.ID.MarshalAdc()
}

func (m *Disconnect) UnmarshalAdc(data []byte) error {
	return m.ID.UnmarshalAdc(data)
}

var _ Message = HubInfo{}

type HubInfo struct {
	Name        string   `adc:"NI,req"`
	Version     string   `adc:"VE,req"`
	Application string   `adc:"AP"`
	Desc        string   `adc:"DE"`
	Type        UserType `adc:"CT"`

	// PING extension

	Address    string `adc:"HH"` // Hub Host address (ADC/ADCS URL address form)
	Website    string `adc:"WS"` // Hub Website
	Network    string `adc:"NE"` // Hub Network
	Owner      string `adc:"OW"` // Hub Owner name
	Users      int    `adc:"UC"` // Current User count, required
	Share      int    `adc:"SS"` // Total share size
	Files      int    `adc:"SF"` // Total files shared
	MinShare   int    `adc:"MS"` // Minimum share required to enter hub ( bytes )
	MaxShare   int64  `adc:"XS"` // Maximum share for entering hub ( bytes )
	MinSlots   int    `adc:"ML"` // Minimum slots required to enter hub
	MaxSlots   int    `adc:"XL"` // Maximum slots for entering hub
	UsersLimit int    `adc:"MC"` // Maximum possible clients ( users ) who can connect
	Uptime     int    `adc:"UP"` // Hub uptime (seconds)

	// ignored, doesn't matter in practice

	//int `adc:"MU"` // Minimum hubs connected where clients can be users
	//int `adc:"MR"` // Minimum hubs connected where client can be registered
	//int `adc:"MO"` // Minimum hubs connected where client can be operators
	//int `adc:"XU"` // Maximum hubs connected where clients can be users
	//int `adc:"XR"` // Maximum hubs connected where client can be registered
	//int `adc:"XO"` // Maximum hubs connected where client can be operators
}

func (HubInfo) Cmd() MsgType {
	// TODO: it's the same as User, so we won't register this one
	return MsgType{'I', 'N', 'F'}
}

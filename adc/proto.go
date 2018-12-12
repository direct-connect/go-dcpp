package adc

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/dennwc/go-dcpp/tiger"
)

// https://adc.sourceforge.io/ADC.html

var (
	CmdInfo           = CmdName{'I', 'N', 'F'}
	CmdSupport        = CmdName{'S', 'U', 'P'}
	CmdSession        = CmdName{'S', 'I', 'D'}
	CmdStatus         = CmdName{'S', 'T', 'A'}
	CmdDisconnect     = CmdName{'Q', 'U', 'I'}
	CmdMessage        = CmdName{'M', 'S', 'G'}
	CmdConnectToMe    = CmdName{'C', 'T', 'M'}
	CmdRevConnectToMe = CmdName{'R', 'C', 'M'}
	CmdGet            = CmdName{'G', 'E', 'T'}
	CmdSend           = CmdName{'S', 'N', 'D'}
	CmdPass           = CmdName{'P', 'A', 'S'}
	CmdAuth           = CmdName{'G', 'P', 'A'}
	CmdSearch         = CmdName{'S', 'C', 'H'}
	CmdResult         = CmdName{'R', 'E', 'S'}
	CmdFileInfo       = CmdName{'G', 'F', 'I'}
)

const (
	searchAnd         = "AN"
	searchNot         = "NO"
	searchExt         = "EX"
	searchSizeLess    = "LE"
	searchSizeGreater = "GE"
	searchSizeEqual   = "EQ"
	searchToken       = "TO"
	searchType        = "TY"
	searchTTH         = "TR"
	searchTTHDepth    = "TD"
)

type AddFeatures []string

func (f AddFeatures) MarshalAdc() (string, error) {
	s := ""
	for i, sf := range f {
		if i > 0 {
			s += " AD" + sf
		} else {
			s += "AD" + sf
		}
	}
	return s, nil
}

type Error struct {
	Status
}

func (e Error) Error() string {
	return fmt.Sprintf("code %d: %s", e.Code, e.Msg)
}

type Severity int

const (
	Success     = Severity(0)
	Recoverable = Severity(1)
	Fatal       = Severity(2)
)

type Status struct {
	Sev  Severity
	Code int
	Msg  string
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

type String string

func (f *String) UnmarshalAdc(s []byte) error {
	*f = String(unescape(s))
	return nil
}
func (f String) MarshalAdc() ([]byte, error) {
	return escape(string(f)), nil
}

type Feature [4]byte

func (f Feature) String() string {
	return string(f[:])
}

func (f *Feature) UnmarshalAdc(s []byte) error {
	if len(s) != 4 {
		return fmt.Errorf("malformed feature [%d]", len(s))
	}
	var v Feature
	copy(v[:], s)
	*f = v
	return nil
}
func (f Feature) MarshalAdc() ([]byte, error) {
	return f[:], nil
}

type ModFeatures map[Feature]bool

func (f ModFeatures) Clone() ModFeatures {
	mf := make(ModFeatures, len(f))
	for k, v := range f {
		mf[k] = v
	}
	return mf
}
func (f *ModFeatures) UnmarshalAdc(s []byte) error {
	// TODO: will parse strings of any length; should limit to 4 bytes
	// TODO: use bytes, or Feature in slices
	var out struct {
		Add []string `adc:"AD"`
		Rm  []string `adc:"RM"`
	}
	if err := Unmarshal(s, &out); err != nil {
		return err
	}
	m := *f
	if m == nil {
		m = make(ModFeatures)
		*f = m
	}
	for _, name := range out.Rm {
		var fea Feature
		if err := fea.UnmarshalAdc([]byte(name)); err != nil {
			return err
		}
		m[fea] = false
	}
	for _, name := range out.Add {
		var fea Feature
		if err := fea.UnmarshalAdc([]byte(name)); err != nil {
			return err
		}
		m[fea] = true
	}
	return nil
}
func (f ModFeatures) MarshalAdc() ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	first := true
	for fea, st := range f {
		if !first {
			buf.WriteRune(' ')
		} else {
			first = false
		}
		if st {
			buf.WriteString("AD")
		} else {
			buf.WriteString("RM")
		}
		buf.WriteString(fea.String())
	}
	return buf.Bytes(), nil
}
func (f ModFeatures) IsSet(s Feature) bool {
	if f == nil {
		return false
	}
	_, ok := f[s]
	return ok
}
func (f ModFeatures) SetFrom(fp ModFeatures) ModFeatures {
	if f == nil && fp == nil {
		return nil
	}
	fi := f.Clone()
	for name, add := range fp {
		fi[name] = add
	}
	return fi
}
func (f ModFeatures) Intersect(fp ModFeatures) ModFeatures {
	if f == nil || fp == nil {
		return nil
	}
	fi := make(ModFeatures)
	for name, add := range f {
		if add {
			if sup, ok := fp[name]; sup && ok {
				fi[name] = true
			}
		}
	}
	return fi
}
func (f ModFeatures) Join() string {
	var arr []string
	for name, add := range f {
		if add {
			arr = append(arr, name.String())
		}
	}
	return strings.Join(arr, ",")
}

type HubInfo struct {
	Name    string   `adc:"NI"`
	Version string   `adc:"VE"`
	Desc    string   `adc:"DE"`
	Type    UserType `adc:"CT"`

	// PING extension

	Address    string `adc:"HH"` // Hub Host address (ADC/ADCS URL address form)
	Website    string `adc:"WS"` // Hub Website
	Network    string `adc:"NE"` // Hub Network
	Owner      string `adc:"OW"` // Hub Owner name
	Users      int    `adc:"UC"` // Current User count
	Share      int    `adc:"SS"` // Total share size
	Files      int    `adc:"SF"` // Total files shared
	MinShare   int    `adc:"MS"` // Minimum share required to enter hub ( bytes )
	MaxShare   int    `adc:"XS"` // Maximum share for entering hub ( bytes )
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

type User struct {
	Id   CID    `adc:"ID"`
	Pid  *PID   `adc:"PD"` // sent only to hub
	Name string `adc:"NI"`

	Ip4  string `adc:"I4"`
	Ip6  string `adc:"I6"`
	Udp4 int    `adc:"U4"`
	Udp6 int    `adc:"U6"`

	ShareSize  int64 `adc:"SS"`
	ShareFiles int   `adc:"SF"`

	Version     string `adc:"VE"`
	Application string `adc:"AP"`

	MaxUpload   string `adc:"US"` // TODO: most time it's int, but some clients write things like "Cable"
	MaxDownload int    `adc:"DS"`

	Slots         int `adc:"SL"`
	SlotsFree     int `adc:"FS"`
	AutoSlotLimit int `adc:"AS"`

	Email string `adc:"EM"`
	Desc  string `adc:"DE"`

	HubsNormal     int `adc:"HN"`
	HubsRegistered int `adc:"HR"`
	HubsOperator   int `adc:"HO"`

	Token string `adc:"TO"` // C-C only

	Type UserType `adc:"CT"`
	Away AwayType `adc:"AW"`
	Ref  string   `adc:"RF"`

	Features ExtFeatures `adc:"SU"`

	KP string `adc:"KP"`
}

var (
	_ Marshaller   = (ExtFeatures)(nil)
	_ Unmarshaller = (*ExtFeatures)(nil)
)

type ExtFeatures []Feature

func (f ExtFeatures) Has(s Feature) bool {
	for _, sf := range f {
		if sf == s {
			return true
		}
	}
	return false
}
func (f *ExtFeatures) UnmarshalAdc(s []byte) error {
	sub := bytes.Split(s, []byte(","))
	arr := make(ExtFeatures, 0, len(sub))
	for _, s := range sub {
		var fea Feature
		if err := fea.UnmarshalAdc(s); err != nil {
			return err
		}
		arr = append(arr, fea)
	}
	*f = arr
	return nil
}
func (f ExtFeatures) MarshalAdc() ([]byte, error) {
	sub := make([][]byte, 0, len(f))
	for _, fea := range f {
		s, err := fea.MarshalAdc()
		if err != nil {
			return nil, err
		}
		sub = append(sub, s)
	}
	return bytes.Join(sub, []byte(",")), nil
}

type BoolInt bool

func (f *BoolInt) UnmarshalAdc(s []byte) error {
	if len(s) != 1 {
		return fmt.Errorf("wrong bool value: '%s'", s)
	}
	switch s[0] {
	case '0':
		*f = false
	case '1':
		*f = true
	default:
		return fmt.Errorf("wrong bool value: '%s'", s)
	}
	return nil
}
func (f BoolInt) MarshalAdc() ([]byte, error) {
	if bool(f) {
		return []byte("1"), nil
	}
	return []byte("0"), nil
}

type UserType int

func (t UserType) Is(st UserType) bool { return t&st != 0 }

const (
	UserTypeNone       UserType = 0x00
	UserTypeBot        UserType = 0x01
	UserTypeRegistered UserType = 0x02
	UserTypeOperator   UserType = 0x04
	UserTypeSuperUser  UserType = 0x08
	UserTypeHubOwner   UserType = 0x10
	UserTypeHub        UserType = 0x20
	UserTypeHidden     UserType = 0x40
)

type AwayType int

const (
	AwayTypeNone     AwayType = 0
	AwayTypeNormal   AwayType = 1
	AwayTypeExtended AwayType = 2
)

type SearchResult struct {
	Token string `adc:"TO"`
	Path  string `adc:"FN"`
	Size  int64  `adc:"SI"`
	Slots int    `adc:"SL"`

	// TIGR ext
	Tiger TTH `adc:"TR"`
}

type FileType int

const (
	FileTypeAny  FileType = 0
	FileTypeFile FileType = 1
	FileTypeDir  FileType = 2
)

type SearchParams struct {
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

const (
	ProtoADC = `ADC/1.0`
)

type PM struct {
	Text string `adc:"#"`
	Src  SID    `adc:"PM"`
}

type RCMParams struct {
	Proto string `adc:"#"`
	Token string `adc:"#"`
}

type RCMResponse struct {
	Proto string `adc:"#"`
	Port  int    `adc:"#"`
	Token string `adc:"#"`
}

const (
	ExtNone  ExtGroup = 0x00
	ExtAudio ExtGroup = 0x01
	ExtArch  ExtGroup = 0x02
	ExtDoc   ExtGroup = 0x04
	ExtExe   ExtGroup = 0x08
	ExtImage ExtGroup = 0x10
	ExtVideo ExtGroup = 0x20
)

type ExtGroup int

func (t ExtGroup) Is(st ExtGroup) bool { return t&st != 0 }

type FileListDir struct {
	Name       string         `xml:"Name,attr"`
	Incomplete int            `xml:"Incomplete,attr"`
	Dirs       []FileListDir  `xml:"Directory"`
	Files      []FileListFile `xml:"File"`
}

type FileListFile struct {
	Name string     `xml:"Name,attr"`
	Size int64      `xml:"Size,attr"`
	TTH  tiger.Hash `xml:"TTH,attr"`
}

type FileList struct {
	Version   int            `xml:"Version,attr"`
	CID       CID            `xml:"CID,attr"`
	Base      string         `xml:"Base,attr"`
	Generator string         `xml:"Generator,attr"`
	Dirs      []FileListDir  `xml:"Directory"`
	Files     []FileListFile `xml:"File"`
}

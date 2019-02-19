package adc

import (
	"bytes"
	"fmt"
	"strings"
)

// https://adc.sourceforge.io/ADC.html

const (
	ProtoADC  = `ADC/1.0`
	ProtoADCS = `ADCS/0.10`

	SchemaADC  = "adc"
	SchemaADCS = "adcs"
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

var (
	_ Marshaler   = (ExtFeatures)(nil)
	_ Unmarshaler = (*ExtFeatures)(nil)
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
	if len(s) < 1 {
		return nil
	}
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

type PM struct {
	Text string `adc:"#"`
	Src  SID    `adc:"PM"`
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

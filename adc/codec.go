package adc

import (
	"bytes"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/dennwc/go-dcpp/tiger"
)

var (
	_ Unmarshaller = (*tiger.Hash)(nil)
	_ Marshaller   = tiger.Hash{}
)

var (
	escaper   = strings.NewReplacer(`\`, `\\`, " ", `\s`, "\n", `\n`)
	unescaper = strings.NewReplacer(`\s`, " ", `\n`, "\n", `\\`, `\`)
)

func escape(s string) []byte {
	return []byte(escaper.Replace(s))
}

func unescape(s []byte) string {
	return unescaper.Replace(string(s))
}

type Unmarshaller interface {
	UnmarshalAdc(s []byte) error
}

type Marshaller interface {
	MarshalAdc() ([]byte, error)
}

func unmarshalValue(s []byte, rv reflect.Value) error {
	switch fv := rv.Addr().Interface().(type) {
	case Unmarshaller:
		return fv.UnmarshalAdc(s)
	}
	if len(s) == 0 {
		rv.Set(reflect.Zero(rv.Type()))
		return nil
	}
	switch rv.Kind() {
	case reflect.Int:
		vi, err := strconv.Atoi(string(s))
		if err != nil {
			return err
		}
		rv.Set(reflect.ValueOf(vi).Convert(rv.Type()))
		return nil
	case reflect.Int64:
		vi, err := strconv.ParseInt(string(s), 10, 64)
		if err != nil {
			return err
		}
		rv.Set(reflect.ValueOf(vi).Convert(rv.Type()))
		return nil
	case reflect.String:
		rv.Set(reflect.ValueOf(unescape(s)).Convert(rv.Type()))
		return nil
	case reflect.Ptr:
		if len(s) == 0 {
			return nil
		}
		nv := reflect.New(rv.Type().Elem())
		err := unmarshalValue(s, nv.Elem())
		if err == nil {
			rv.Set(nv)
		}
		return err
	}
	return fmt.Errorf("unknown type: %v", rv.Type())
}

func Unmarshal(s []byte, o interface{}) error {
	if m, ok := o.(Unmarshaller); ok {
		return m.UnmarshalAdc(s)
	}
	rv := reflect.ValueOf(o)
	if rv.Kind() != reflect.Ptr {
		return fmt.Errorf("pointer expected, got: %T", o)
	}
	rv = rv.Elem()
	if rv.Kind() != reflect.Struct {
		return fmt.Errorf("struct expected, got: %T", o)
	}
	rt := rv.Type()

	sub := bytes.Split(s, []byte(" "))
	for i := 0; i < rt.NumField(); i++ {
		fld := rt.Field(i)
		tag := fld.Tag.Get(`adc`)
		if tag == "" || tag == "-" {
			continue
		} else if tag == "#" {
			v := sub[0]
			sub = sub[1:]
			if err := unmarshalValue(v, rv.Field(i)); err != nil {
				return fmt.Errorf("error on field %s: %s", fld.Name, err)
			}
			continue
		}
		btag := []byte(tag)
		var vals [][]byte
		for _, ss := range sub {
			if bytes.HasPrefix(ss, btag) {
				vals = append(vals, bytes.TrimPrefix(ss, btag))
			}
		}
		if len(vals) > 0 {
			if m, ok := rv.Field(i).Addr().Interface().(Unmarshaller); ok {
				if err := m.UnmarshalAdc(vals[0]); err != nil {
					return fmt.Errorf("error on field %s: %s", fld.Name, err)
				}
			} else if fld.Type.Kind() == reflect.Slice {
				nv := reflect.MakeSlice(fld.Type, len(vals), len(vals))
				for j, sv := range vals {
					if err := unmarshalValue(sv, nv.Index(j)); err != nil {
						return fmt.Errorf("error on field %s: %s", fld.Name, err)
					}
				}
				rv.Field(i).Set(nv)
			} else {
				if len(vals) > 1 {
					return fmt.Errorf("error on field %s: expected single value", fld.Name)
				} else {
					if err := unmarshalValue(vals[0], rv.Field(i)); err != nil {
						return fmt.Errorf("error on field %s: %s", fld.Name, err)
					}
				}
			}
		}
	}
	return nil
}

func marshalValue(o interface{}) ([]byte, error) {
	switch v := o.(type) {
	case Marshaller:
		return v.MarshalAdc()
	}
	rv := reflect.ValueOf(o)
	switch rv.Kind() {
	case reflect.String:
		s := rv.Convert(reflect.TypeOf(string(""))).Interface().(string)
		return escape(s), nil
	case reflect.Int:
		v := rv.Convert(reflect.TypeOf(int(0))).Interface().(int)
		return []byte(strconv.Itoa(v)), nil
	case reflect.Int64:
		v := rv.Convert(reflect.TypeOf(int64(0))).Interface().(int64)
		return []byte(strconv.FormatInt(v, 10)), nil
	case reflect.Ptr:
		if rv.IsNil() {
			return nil, nil
		}
		return marshalValue(rv.Elem().Interface())
	}
	return nil, fmt.Errorf("unsupported type: %T", o)
}

func isZero(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Ptr, reflect.Slice:
		return v.IsNil()
	default:
		return v.Interface() == reflect.Zero(v.Type()).Interface()
	}
}

func Marshal(o interface{}) ([]byte, error) {
	if o == nil {
		return nil, nil
	}
	if m, ok := o.(Marshaller); ok {
		return m.MarshalAdc()
	}
	rv := reflect.ValueOf(o)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil, fmt.Errorf("struct expected, got: %T", o)
	}
	buf := bytes.NewBuffer(nil)
	rt := rv.Type()
	first := true
	writeItem := func(tag string, sv []byte) {
		if !first {
			buf.WriteRune(' ')
		} else {
			first = false
		}
		if tag == `#` {
			buf.Write(sv)
		} else {
			buf.WriteString(tag)
			buf.Write(sv)
		}
	}
	for i := 0; i < rt.NumField(); i++ {
		fld := rt.Field(i)
		tag := fld.Tag.Get(`adc`)
		if tag == "" || tag == "-" {
			continue
		}
		omit := tag != "#"
		if omit && isZero(rv.Field(i)) {
			continue
		}
		if m, ok := rv.Field(i).Interface().(Marshaller); ok {
			sv, err := m.MarshalAdc()
			if err != nil {
				return nil, fmt.Errorf("cannot marshal field %s: %v", fld.Name, err)
			}
			writeItem(tag, sv)
			continue
		}
		if tag != "#" && fld.Type.Kind() == reflect.Slice {
			for j := 0; j < rv.Field(i).Len(); j++ {
				sv, err := marshalValue(rv.Field(i).Index(j).Interface())
				if err != nil {
					return nil, fmt.Errorf("cannot marshal field %s: %v", fld.Name, err)
				}
				writeItem(tag, sv)
			}
		} else {
			sv, err := marshalValue(rv.Field(i).Interface())
			if err != nil {
				return nil, fmt.Errorf("cannot marshal field %s: %v", fld.Name, err)
			}
			writeItem(tag, sv)
		}
	}
	return buf.Bytes(), nil
}

func MustMarshal(o interface{}) []byte {
	data, err := Marshal(o)
	if err != nil {
		panic(err)
	}
	return data
}

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

type Command interface {
	GetName() CmdName
	Data() []byte
	Marshaller
}

type ClientCommand interface {
	Command
	SetSID(sid SID)
}

type CmdName [3]byte

func (s CmdName) String() string { return string(s[:]) }

type Broadcast struct {
	Name CmdName
	Id   SID
	Raw  []byte
}

func (cmd Broadcast) GetName() CmdName { return cmd.Name }
func (cmd Broadcast) Data() []byte     { return cmd.Raw }
func (cmd Broadcast) MarshalAdc() ([]byte, error) {
	n := 9
	if len(cmd.Raw) > 0 {
		n += 1 + len(cmd.Raw)
	}
	buf := make([]byte, n)
	buf[0] = kindBroadcast
	copy(buf[1:], cmd.Name[:])
	buf[4] = ' '
	id, _ := cmd.Id.MarshalAdc()
	copy(buf[5:], id[:])
	if len(cmd.Raw) > 0 {
		buf[9] = ' '
		copy(buf[10:], cmd.Raw)
	}
	return buf, nil
}

type ClientCmd struct {
	Name CmdName
	Raw  []byte
}

func (cmd ClientCmd) GetName() CmdName { return cmd.Name }
func (cmd ClientCmd) Data() []byte     { return cmd.Raw }
func (cmd ClientCmd) MarshalAdc() ([]byte, error) {
	n := 4
	if len(cmd.Raw) > 0 {
		n += 1 + len(cmd.Raw)
	}
	buf := make([]byte, n)
	buf[0] = kindClient
	copy(buf[1:], cmd.Name[:])
	if len(cmd.Raw) > 0 {
		buf[4] = ' '
		copy(buf[5:], cmd.Raw)
	}
	return buf, nil
}

type InfoCmd struct {
	Name CmdName
	Raw  []byte
}

func (cmd InfoCmd) GetName() CmdName { return cmd.Name }
func (cmd InfoCmd) Data() []byte     { return cmd.Raw }
func (cmd InfoCmd) MarshalAdc() ([]byte, error) {
	n := 4
	if len(cmd.Raw) > 0 {
		n += 1 + len(cmd.Raw)
	}
	buf := make([]byte, n)
	buf[0] = kindInfo
	copy(buf[1:], cmd.Name[:])
	if len(cmd.Raw) > 0 {
		buf[4] = ' '
		copy(buf[5:], cmd.Raw)
	}
	return buf, nil
}

type HubCmd struct {
	Name CmdName
	Raw  []byte
}

func (cmd HubCmd) GetName() CmdName { return cmd.Name }
func (cmd HubCmd) Data() []byte     { return cmd.Raw }
func (cmd HubCmd) MarshalAdc() ([]byte, error) {
	n := 4
	if len(cmd.Raw) > 0 {
		n += 1 + len(cmd.Raw)
	}
	buf := make([]byte, n)
	buf[0] = kindHub
	copy(buf[1:], cmd.Name[:])
	if len(cmd.Raw) > 0 {
		buf[4] = ' '
		copy(buf[5:], cmd.Raw)
	}
	return buf, nil
}

var _ ClientCommand = (*DirectCmd)(nil)

type DirectCmd struct {
	Name CmdName
	Id   SID
	Targ SID
	Raw  []byte
}

func (cmd DirectCmd) GetName() CmdName { return cmd.Name }
func (cmd DirectCmd) Data() []byte     { return cmd.Raw }
func (cmd *DirectCmd) SetSID(sid SID) {
	cmd.Id = sid
}
func (cmd DirectCmd) MarshalAdc() ([]byte, error) {
	n := 14
	if len(cmd.Raw) > 0 {
		n += 1 + len(cmd.Raw)
	}
	buf := make([]byte, n)
	buf[0] = kindDirect
	copy(buf[1:], cmd.Name[:])
	buf[4] = ' '
	id, _ := cmd.Id.MarshalAdc()
	copy(buf[5:], id[:])
	buf[9] = ' '
	targ, _ := cmd.Targ.MarshalAdc()
	copy(buf[10:], targ[:])
	if len(cmd.Raw) > 0 {
		buf[14] = ' '
		copy(buf[15:], cmd.Raw)
	}
	return buf, nil
}

type EchoCmd struct {
	Name CmdName
	Id   SID
	Targ SID
	Raw  []byte
}

func (cmd EchoCmd) GetName() CmdName { return cmd.Name }
func (cmd EchoCmd) Data() []byte     { return cmd.Raw }
func (cmd EchoCmd) MarshalAdc() ([]byte, error) {
	n := 14
	if len(cmd.Raw) > 0 {
		n += 1 + len(cmd.Raw)
	}
	buf := make([]byte, n)
	buf[0] = kindEcho
	copy(buf[1:], cmd.Name[:])
	buf[4] = ' '
	id, _ := cmd.Id.MarshalAdc()
	copy(buf[5:], id[:])
	buf[9] = ' '
	targ, _ := cmd.Targ.MarshalAdc()
	copy(buf[10:], targ[:])
	if len(cmd.Raw) > 0 {
		buf[14] = ' '
		copy(buf[15:], cmd.Raw)
	}
	return buf, nil
}

type FeatureCmd struct {
	Name     CmdName
	Id       SID
	Features map[string]bool
	Raw      []byte
}

func (cmd FeatureCmd) GetName() CmdName { return cmd.Name }
func (cmd FeatureCmd) Data() []byte     { return cmd.Raw }
func (cmd FeatureCmd) MarshalAdc() ([]byte, error) {
	n := 9
	if len(cmd.Raw) > 0 {
		n += 1 + len(cmd.Raw)
	}
	for k := range cmd.Features {
		n += 2 + len(k)
	}
	buf := make([]byte, n)
	buf[0] = kindFeature
	copy(buf[1:], cmd.Name[:])
	buf[4] = ' '
	id, _ := cmd.Id.MarshalAdc()
	copy(buf[5:], id[:])
	off := 9
	for k, v := range cmd.Features {
		buf[off] = ' '
		if v {
			buf[off+1] = '+'
		} else {
			buf[off+1] = '-'
		}
		copy(buf[off+2:], k)
		off += 2 + len(k)
	}
	if len(cmd.Raw) > 0 {
		buf[off] = ' '
		copy(buf[off+1:], cmd.Raw)
	}
	return buf, nil
}

type UdpCmd struct {
	Name CmdName
	Id   CID
	Raw  []byte
}

func (cmd UdpCmd) GetName() CmdName { return cmd.Name }
func (cmd UdpCmd) Data() []byte     { return cmd.Raw }
func (cmd UdpCmd) MarshalAdc() ([]byte, error) {
	n := 39 + 5
	if len(cmd.Raw) > 0 {
		n += 1 + len(cmd.Raw)
	}
	buf := make([]byte, n)
	buf[0] = kindUDP
	copy(buf[1:], cmd.Name[:])
	buf[4] = ' '
	copy(buf[5:], cmd.Id.ToBase32())
	if len(cmd.Raw) > 0 {
		buf[5+39] = ' '
		copy(buf[5+39+1:], cmd.Raw)
	}
	return buf, nil
}

func NewBroadcast(name CmdName, sid SID, o interface{}) Broadcast {
	return Broadcast{Name: name, Id: sid, Raw: []byte(MustMarshal(o))}
}

func NewHubCmd(name CmdName, o interface{}) HubCmd {
	return HubCmd{Name: name, Raw: []byte(MustMarshal(o))}
}

func NewInfoCmd(name CmdName, o interface{}) InfoCmd {
	return InfoCmd{Name: name, Raw: []byte(MustMarshal(o))}
}

func NewFeatureCmd(name CmdName, sid SID, fm map[string]bool, o interface{}) FeatureCmd {
	return FeatureCmd{Name: name, Id: sid, Features: fm, Raw: []byte(MustMarshal(o))}
}

func NewDirectCmd(name CmdName, sid SID, targ SID, o interface{}) DirectCmd {
	return DirectCmd{Name: name, Id: sid, Targ: targ, Raw: []byte(MustMarshal(o))}
}

func NewEchoCmd(name CmdName, sid SID, targ SID, o interface{}) EchoCmd {
	return EchoCmd{Name: name, Id: sid, Targ: targ, Raw: []byte(MustMarshal(o))}
}

func NewClientCmd(name CmdName, o interface{}) ClientCmd {
	return ClientCmd{Name: name, Raw: []byte(MustMarshal(o))}
}

func DecodeCmd(p []byte) (Command, error) {
	if len(p) < 4 {
		return nil, fmt.Errorf("too short for command: '%s'", string(p))
	}
	kind := rune(p[0])
	switch kind {
	case kindBroadcast,
		kindClient,
		kindDirect,
		kindEcho,
		kindFeature,
		kindHub,
		kindInfo,
		kindUDP:
	default:
		return nil, fmt.Errorf("unknown command kind: %c", p[0])
	}
	var cname CmdName
	copy(cname[:], p[1:4])
	p = p[4:]
	var raw []byte
	if len(p) > 0 {
		if p[0] != ' ' {
			return nil, fmt.Errorf("name separator expected")
		}
		raw = p[1:]
	}
	switch kind {
	case kindBroadcast: // BINF AAAA <data>
		if len(raw) < 4 {
			return nil, fmt.Errorf("short broadcast")
		} else if len(raw) > 4 && raw[4] != ' ' {
			return nil, fmt.Errorf("separator expected: '%s'", string(raw[:5]))
		}
		cmd := Broadcast{Name: cname}
		if err := cmd.Id.UnmarshalAdc(raw[0:4]); err != nil {
			return nil, err
		}
		if len(raw) > 5 {
			cmd.Raw = raw[5:]
		}
		return cmd, nil
	case kindClient: // CINF <data>
		return ClientCmd{Name: cname, Raw: raw}, nil
	case kindInfo: // IINF <data>
		return InfoCmd{Name: cname, Raw: raw}, nil
	case kindHub: // HINF <data>
		return HubCmd{Name: cname, Raw: raw}, nil
	case kindDirect, kindEcho: // DCTM AAAA BBBB <data>
		if len(raw) < 9 {
			return nil, fmt.Errorf("short direct/echo command")
		} else if raw[4] != ' ' {
			return nil, fmt.Errorf("separator expected: '%s'", string(raw[:9]))
		} else if len(raw) > 9 && raw[9] != ' ' {
			return nil, fmt.Errorf("separator expected: '%s'", string(raw[:10]))
		}
		if kind == kindDirect {
			cmd := DirectCmd{Name: cname}
			if err := cmd.Id.UnmarshalAdc(raw[0:4]); err != nil {
				return nil, err
			}
			if err := cmd.Targ.UnmarshalAdc(raw[5:9]); err != nil {
				return nil, err
			}
			if len(raw) > 10 {
				cmd.Raw = raw[10:]
			}
			return cmd, nil
		} else {
			cmd := EchoCmd{Name: cname}
			if err := cmd.Id.UnmarshalAdc(raw[0:4]); err != nil {
				return nil, err
			}
			if err := cmd.Targ.UnmarshalAdc(raw[5:9]); err != nil {
				return nil, err
			}
			if len(raw) > 10 {
				cmd.Raw = raw[10:]
			}
			return cmd, nil
		}
	case kindFeature: // FSCH AAAA +SEGA -NAT0 <data>
		if len(raw) < 4 {
			return nil, fmt.Errorf("short feature command")
		} else if len(raw) > 4 && raw[4] != ' ' {
			return nil, fmt.Errorf("separator expected: '%s'", string(raw[:5]))
		}
		cmd := FeatureCmd{Name: cname, Features: make(map[string]bool)}
		if err := cmd.Id.UnmarshalAdc(raw[0:4]); err != nil {
			return nil, err
		}
		if len(raw) > 5 {
			raw = raw[5:]
		} else {
			raw = nil
		}
		for i := 0; i < len(raw); i++ {
			if raw[i] == '+' {
				if f := raw[i:]; len(f) < 5 {
					return nil, fmt.Errorf("short feature: '%s'", string(raw[i:i+5]))
				}
				cmd.Features[string(raw[i+1:i+5])] = true
				i += 4
			} else if raw[i] == '-' {
				if f := raw[i:]; len(f) < 5 {
					return nil, fmt.Errorf("short feature: '%s'", string(raw[i:i+5]))
				}
				cmd.Features[string(raw[i+1:i+5])] = false
				i += 4
			} else if raw[i] == ' ' {
				raw = raw[i:]
				i = 0
			} else {
				raw = raw[i:]
				break
			}
			if i+1 == len(raw) {
				raw = nil
			}
		}
		if len(cmd.Features) == 0 {
			cmd.Features = nil
		}
		if len(raw) > 0 {
			cmd.Raw = raw
		}
		return cmd, nil
	case kindUDP: // UINF <CID> <data>
		const l = 39 // len of CID in base32
		if len(raw) < l {
			return nil, fmt.Errorf("short upd command")
		} else if len(raw) > l && raw[l] != ' ' {
			return nil, fmt.Errorf("separator expected: '%s'", string(raw[:l+1]))
		}
		cmd := UdpCmd{Name: cname}
		if err := cmd.Id.FromBase32(string(raw[0:l])); err != nil {
			return nil, fmt.Errorf("wrong CID in upd command: %v", err)
		}
		if len(raw) > l+1 {
			cmd.Raw = raw[l+1:]
		}
		return cmd, nil
	default:
		return nil, fmt.Errorf("unknown command kind: %c", kind)
	}
}

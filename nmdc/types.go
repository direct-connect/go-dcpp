package nmdc

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding"
)

type NoArgs struct{}

func (*NoArgs) MarshalNMDC(_ *encoding.Encoder) ([]byte, error) {
	return nil, nil
}

func (*NoArgs) UnmarshalNMDC(_ *encoding.Decoder, data []byte) error {
	if len(data) != 0 {
		return errors.New("unexpected argument for the command")
	}
	return nil
}

type Name string

func (s Name) MarshalNMDC(enc *encoding.Encoder) ([]byte, error) {
	if len(s) > maxName {
		return nil, errors.New("name is too long")
	} else if strings.ContainsAny(string(s), invalidCharsName) {
		return nil, fmt.Errorf("invalid characters in name: %q", string(s))
	}
	str := string(s)
	if enc != nil {
		var err error
		str, err = enc.String(str)
		if err != nil {
			return nil, err
		}
	}
	str = EscapeName(str)
	return []byte(str), nil
}

func (s *Name) UnmarshalNMDC(dec *encoding.Decoder, data []byte) error {
	if len(data) > maxName {
		return errors.New("name is too long")
	} else if bytes.ContainsAny(data, invalidCharsName) {
		return fmt.Errorf("invalid characters in name: %q", string(data))
	}
	str := Unescape(string(data))
	if dec != nil {
		var err error
		str, err = dec.String(str)
		if err != nil {
			return err
		}
	}
	if !utf8.ValidString(str) {
		return &errUnknownEncoding{text: []byte(str)}
	}
	*s = Name(str)
	return nil
}

type String string

func (s String) MarshalNMDC(enc *encoding.Encoder) ([]byte, error) {
	if strings.ContainsAny(string(s), "\x00") {
		return nil, errors.New("invalid characters in text")
	}
	str := string(s)
	if enc != nil {
		var err error
		str, err = enc.String(str)
		if err != nil {
			return nil, err
		}
	}
	str = Escape(str)
	return []byte(str), nil
}

func (s *String) UnmarshalNMDC(dec *encoding.Decoder, data []byte) error {
	if bytes.ContainsAny(data, "\x00") {
		return errors.New("invalid characters in text")
	}
	str := Unescape(string(data))
	if dec != nil {
		var err error
		str, err = dec.String(str)
		if err != nil {
			return err
		}
	}
	if !utf8.ValidString(str) {
		return &errUnknownEncoding{text: []byte(str)}
	}
	*s = String(str)
	return nil
}

type Features map[string]struct{}

func (f Features) Has(name string) bool {
	_, ok := f[name]
	return ok
}

func (f Features) Set(name string) {
	f[name] = struct{}{}
}

func (f Features) Clone() Features {
	f2 := make(Features, len(f))
	for name := range f {
		f2[name] = struct{}{}
	}
	return f2
}

func (f Features) Intersect(f2 Features) Features {
	m := make(Features)
	for name := range f2 {
		if _, ok := f[name]; ok {
			m[name] = struct{}{}
		}
	}
	return m
}

func (f Features) IntersectList(f2 []string) Features {
	m := make(Features)
	for _, name := range f2 {
		if _, ok := f[name]; ok {
			m[name] = struct{}{}
		}
	}
	return m
}

func (f Features) List() []string {
	arr := make([]string, 0, len(f))
	for s := range f {
		arr = append(arr, s)
	}
	sort.Strings(arr)
	return arr
}

package nmdc

import (
	"bytes"
	"errors"
	"sort"
	"strings"
)

type Name string

func (s Name) MarshalNMDC() ([]byte, error) {
	if len(s) > maxName {
		return nil, errors.New("name is too long")
	} else if strings.ContainsAny(string(s), invalidCharsName) {
		return nil, errors.New("invalid characters in name")
	}
	// TODO: encoding
	str := EscapeName(string(s))
	return []byte(str), nil
}
func (s *Name) UnmarshalNMDC(data []byte) error {
	if len(data) > maxName {
		return errors.New("name is too long")
	} else if bytes.ContainsAny(data, invalidCharsName) {
		return errors.New("invalid characters in name")
	}
	// TODO: encoding
	str := Unescape(string(data))
	*s = Name(str)
	return nil
}

type String string

func (s String) MarshalNMDC() ([]byte, error) {
	if len(s) > maxText {
		return nil, errors.New("text is too long")
	} else if strings.ContainsAny(string(s), "\x00") {
		return nil, errors.New("invalid characters in text")
	}
	// TODO: encoding
	str := Escape(string(s))
	return []byte(str), nil
}
func (s *String) UnmarshalNMDC(data []byte) error {
	if len(data) > maxText {
		return errors.New("text is too long")
	} else if bytes.ContainsAny(data, "\x00") {
		return errors.New("invalid characters in text")
	}
	// TODO: encoding
	str := Unescape(string(data))
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

package adc

import (
	"bytes"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"

	"github.com/dennwc/go-dcpp/tiger"
)

var (
	_ Unmarshaler = (*tiger.Hash)(nil)
	_ Marshaler   = tiger.Hash{}
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

type Unmarshaler interface {
	UnmarshalAdc(data []byte) error
}

type Marshaler interface {
	MarshalAdc() ([]byte, error)
}

var (
	_ Marshaler   = CID{}
	_ Unmarshaler = (*CID)(nil)
)

func unmarshalValue(s []byte, rv reflect.Value) error {
	switch fv := rv.Addr().Interface().(type) {
	case Unmarshaler:
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
		sv := string(s)
		vi, err := strconv.ParseInt(sv, 10, 64)
		if err != nil {
			vf, err2 := strconv.ParseFloat(sv, 64)
			if err2 != nil {
				return err
			} else if math.Round(vf) != vf {
				return err2
			}
			vi = int64(vf)
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
	if m, ok := o.(Unmarshaler); ok {
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
		tag := strings.SplitN(fld.Tag.Get(`adc`), ",", 2)[0]
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
			if m, ok := rv.Field(i).Addr().Interface().(Unmarshaler); ok {
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
	case Marshaler:
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
	if m, ok := o.(Marshaler); ok {
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
		sub := strings.SplitN(tag, ",", 2)
		tag = sub[0]
		omit := tag != "#"
		if len(sub) > 1 && sub[1] == "req" {
			omit = false
		}
		if omit && isZero(rv.Field(i)) {
			continue
		}
		if m, ok := rv.Field(i).Interface().(Marshaler); ok {
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

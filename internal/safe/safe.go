package safe

import "sync/atomic"

type Bool struct {
	v uint32
}

func (b *Bool) Get() bool {
	return atomic.LoadUint32(&b.v) != 0
}

func (b *Bool) Set(v bool) {
	if v {
		atomic.StoreUint32(&b.v, 1)
	} else {
		atomic.StoreUint32(&b.v, 0)
	}
}

type String struct {
	v atomic.Value
}

func (s *String) Get() string {
	v := s.v.Load()
	sv, _ := v.(string)
	return sv
}

func (s *String) Set(v string) {
	s.v.Store(v)
}

package safe

import (
	"sync/atomic"
	"time"
)

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

type Time struct {
	v int64
}

func (t *Time) Get() time.Time {
	return time.Unix(0, atomic.LoadInt64(&t.v))
}

func (t *Time) Set(v time.Time) {
	atomic.StoreInt64(&t.v, v.UnixNano())
}

func (t *Time) SetNow() {
	t.Set(time.Now())
}

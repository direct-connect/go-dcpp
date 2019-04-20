package hub

import "sync/atomic"

type sampler struct {
	n   uint32
	fnc atomic.Value // sampleFunc
}

type sampleFunc func(data []byte) bool

func (s *sampler) enabled() bool {
	return atomic.LoadUint32(&s.n) != 0
}

func (s *sampler) sample(data []byte) {
	n := atomic.LoadUint32(&s.n)
	if n == 0 {
		return
	}
	v := s.fnc.Load()
	if v == nil {
		return
	}
	fnc := v.(sampleFunc)
	if fnc == nil {
		return
	}
	if !fnc(data) {
		return
	}
	for {
		n := atomic.LoadUint32(&s.n)
		if n == 0 {
			return
		}
		if atomic.CompareAndSwapUint32(&s.n, n, n-1) {
			if n-1 == 0 {
				s.stop()
			}
			return
		}
	}
}

func (s *sampler) stop() {
	atomic.StoreUint32(&s.n, 0)
	s.fnc.Store((sampleFunc)(nil))
}

func (s *sampler) start(n uint, fnc sampleFunc) {
	s.stop()
	if n == 0 {
		n = 10
	}
	s.fnc.Store(fnc)
	atomic.StoreUint32(&s.n, uint32(n))
}

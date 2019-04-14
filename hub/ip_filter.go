package hub

import (
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const (
	maxViolations  = 100 // per minute
	forgetAfterSec = int64(time.Minute * 10 / time.Second)
)

func addrString(a net.Addr) string {
	switch a := a.(type) {
	case *net.TCPAddr:
		return a.IP.String()
	}
	return a.String()
}

type addrKey string

func minAddrKey(a net.Addr) addrKey {
	switch a := a.(type) {
	case *net.TCPAddr:
		return minIPKey(a.IP)
	}
	return addrKey(a.String())
}

func minIPKey(ip net.IP) addrKey {
	if ip4 := ip.To4(); ip4 != nil {
		return addrKey(ip4)
	}
	return addrKey(ip)
}

type ipInfo struct {
	violations uint64
	last       int64 // sec
}

type ipFilter struct {
	blocked sync.Map // map[addrKey]struct{}
	info    sync.Map // map[addrKey]*ipInfo
}

func (f *ipFilter) run(done <-chan struct{}) {
	// this routines resets all protocol violation counters each minute
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case t := <-ticker.C:
			tsec := t.Unix()
			f.info.Range(func(key, vi interface{}) bool {
				v := vi.(*ipInfo)
				last := atomic.LoadInt64(&v.last)
				if tsec-last > forgetAfterSec {
					f.info.Delete(key)
					return true
				}
				atomic.StoreUint64(&v.violations, 0)
				return true
			})
		}
	}
}

func (f *ipFilter) blockKey(key addrKey) {
	f.blocked.Store(key, struct{}{})
}

func (f *ipFilter) blockAndCheckKey(key addrKey) bool {
	_, loaded := f.blocked.LoadOrStore(key, struct{}{})
	return !loaded
}

func (h *Hub) IsHardBlocked(a net.Addr) bool {
	key := minAddrKey(a)
	_, blocked := h.ipFilter.blocked.Load(key)
	return blocked
}

func (h *Hub) IsHardBlockedIP(ip net.IP) bool {
	key := minIPKey(ip)
	_, blocked := h.ipFilter.blocked.Load(key)
	return blocked
}

func (h *Hub) HardBlock(a net.Addr) {
	key := minAddrKey(a)
	h.ipFilter.blockKey(key)
}

func (h *Hub) HardBlockIP(ip net.IP) {
	key := minIPKey(ip)
	h.ipFilter.blockKey(key)
}

func (h *Hub) reportAutoBlock(a net.Addr, reason error) {
	log.Println("blocked:", addrString(a), "reason:", reason)
}

func (h *Hub) probableAttack(a net.Addr, reason error) {
	key := minAddrKey(a)
	if _, blocked := h.ipFilter.blocked.Load(key); blocked {
		return
	}
	vi, loaded := h.ipFilter.info.LoadOrStore(key, &ipInfo{violations: 1})
	if !loaded {
		return // first violation
	}
	v := vi.(*ipInfo)
	n := atomic.AddUint64(&v.violations, 1)
	if n >= maxViolations {
		if h.ipFilter.blockAndCheckKey(key) {
			h.reportAutoBlock(a, reason)
		}
	} else {
		atomic.StoreInt64(&v.last, time.Now().Unix())
	}
}

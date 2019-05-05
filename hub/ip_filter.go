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

func (k BanKey) ToIP() net.IP {
	if len(k) != net.IPv4len && len(k) != net.IPv6len {
		return nil
	}
	b := make([]byte, len(k))
	copy(b, k)
	return net.IP(b)
}

func MinAddrKey(a net.Addr) BanKey {
	switch a := a.(type) {
	case *net.TCPAddr:
		return MinIPKey(a.IP)
	}
	return BanKey(a.String())
}

func MinIPKey(ip net.IP) BanKey {
	if ip4 := ip.To4(); ip4 != nil {
		return BanKey(ip4)
	}
	return BanKey(ip)
}

type banInfo struct {
	violations uint64
	last       int64 // sec
}

type bans struct {
	blocked sync.Map // map[BanKey]struct{}
	info    sync.Map // map[BanKey]*banInfo
}

func (f *bans) run(done <-chan struct{}) {
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
				v := vi.(*banInfo)
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

func (f *bans) blockKey(key BanKey) {
	f.blocked.Store(key, struct{}{})
}

func (f *bans) unblockKey(key BanKey) {
	f.blocked.Delete(key)
}

func (f *bans) blockAndCheckKey(key BanKey) bool {
	_, loaded := f.blocked.LoadOrStore(key, struct{}{})
	return !loaded
}

func (h *Hub) IsHardBlocked(a net.Addr) bool {
	key := MinAddrKey(a)
	_, blocked := h.bans.blocked.Load(key)
	return blocked
}

func (h *Hub) IsHardBlockedIP(ip net.IP) bool {
	key := MinIPKey(ip)
	_, blocked := h.bans.blocked.Load(key)
	return blocked
}

func (h *Hub) EachHardBlockedIP(fnc func(ip net.IP) bool) {
	h.bans.blocked.Range(func(key, _ interface{}) bool {
		k := key.(BanKey)
		ip := k.ToIP()
		if ip == nil {
			return true
		}
		return fnc(ip)
	})
}

func (h *Hub) HardBlock(a net.Addr) {
	key := MinAddrKey(a)
	h.bans.blockKey(key)
	h.saveBan(Ban{Key: key, Hard: true})
}

func (h *Hub) HardBlockIP(ip net.IP) {
	key := MinIPKey(ip)
	h.bans.blockKey(key)
	h.saveBan(Ban{Key: key, Hard: true})
}

func (h *Hub) HardUnBlockIP(ip net.IP) {
	key := MinIPKey(ip)
	h.bans.blockKey(key)
	h.saveBan(Ban{Key: key, Hard: true})
}

func (h *Hub) hardBlockKey(k BanKey) {
	h.bans.blockKey(BanKey(k))
}

func (h *Hub) saveBan(b Ban) {
	if h.db == nil {
		return
	}
	_ = h.db.PutBans([]Ban{b})
}

func (h *Hub) reportAutoBlock(a net.Addr, reason error) {
	log.Println("blocked:", addrString(a), "reason:", reason)
}

func (h *Hub) probableAttack(a net.Addr, reason error) {
	key := MinAddrKey(a)
	if _, blocked := h.bans.blocked.Load(key); blocked {
		return
	}
	vi, loaded := h.bans.info.LoadOrStore(key, &banInfo{violations: 1})
	if !loaded {
		return // first violation
	}
	v := vi.(*banInfo)
	n := atomic.AddUint64(&v.violations, 1)
	if n >= maxViolations {
		if h.bans.blockAndCheckKey(key) {
			h.reportAutoBlock(a, reason)
		}
	} else {
		atomic.StoreInt64(&v.last, time.Now().Unix())
	}
}

func (h *Hub) loadBans() error {
	if h.db == nil {
		return nil
	}
	bans, err := h.db.ListBans()
	if err != nil {
		return err
	}
	for _, b := range bans {
		h.hardBlockKey(BanKey(b.Key))
	}
	if len(bans) != 0 {
		log.Printf("loaded %d bans", len(bans))
	}
	return nil
}

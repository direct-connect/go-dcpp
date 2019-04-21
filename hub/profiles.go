package hub

import "sync"

const (
	PermOwner     = "owner"
	ProfileParent = "parent"
)

const (
	ProfileNameRoot       = "root"
	ProfileNameRegistered = "user"
	ProfileNameOperator   = "op"
	ProfileNameGuest      = "guest"
)

const (
	FlagOpIcon  = "icon.op"
	FlagRegIcon = "icon.reg"
)

func DefaultProfiles() map[string]Map {
	return map[string]Map{
		ProfileNameRoot: {
			PermOwner:  true,
			FlagOpIcon: true,
		},
		ProfileNameOperator: {
			ProfileParent: ProfileNameRegistered,
			FlagOpIcon:    true,

			PermRoomsList: true,
			PermBroadcast: true,
			PermDrop:      true,
			PermIP:        true,
			PermBanIP:     true,
		},
		ProfileNameRegistered: {
			ProfileParent: ProfileNameGuest,
			FlagRegIcon:   true,

			PermRoomsJoin: true,
		},
		ProfileNameGuest: {},
	}
}

type profiles struct {
	sync.RWMutex
	m map[string]*UserProfile
}

func (h *Hub) loadProfiles() error {
	h.profiles.m = make(map[string]*UserProfile)
	m := h.profiles.m

	var needParent []*UserProfile
	for id, v := range DefaultProfiles() {
		p := &UserProfile{
			id: id,
			m:  v.Clone(),
		}
		if _, ok := p.m[ProfileParent]; ok {
			needParent = append(needParent, p)
		}
		m[id] = p
	}
	if h.db != nil {
		ids, err := h.db.ListProfiles()
		if err != nil {
			return err
		}
		for _, id := range ids {
			pr, err := h.db.GetProfile(id)
			if err != nil {
				return err
			} else if pr == nil {
				continue
			}
			p, ok := m[id]
			if ok {
				for k, v := range pr {
					p.m[k] = v
				}
			} else {
				p := &UserProfile{
					id: id,
					m:  pr,
				}
				if _, ok := p.m[ProfileParent]; ok {
					needParent = append(needParent, p)
				}
				m[id] = p
			}
		}
	}
	for _, p := range needParent {
		par, ok := m[p.GetString(ProfileParent)]
		if ok {
			p.parent = par
		}
	}
	return nil
}

func (h *Hub) Profile(id string) *UserProfile {
	h.profiles.RLock()
	p := h.profiles.m[id]
	h.profiles.RUnlock()
	return p
}

type UserProfile struct {
	id string

	mu     sync.RWMutex
	m      Map
	parent *UserProfile
}

func (p *UserProfile) ID() string {
	return p.id
}

func (p *UserProfile) Parent() *UserProfile {
	p.mu.RLock()
	par := p.parent
	p.mu.RUnlock()
	return par
}

func (p *UserProfile) SetParent(par *UserProfile) *UserProfile {
	p.mu.Lock()
	p.parent = par
	if par != nil {
		if p.m == nil {
			p.m = make(Map)
		}
		p.m[ProfileParent] = par.id
	} else {
		delete(p.m, ProfileParent)
	}
	p.mu.Unlock()
	return par
}

func (p *UserProfile) Get(key string) (interface{}, bool) {
	for p != nil {
		p.mu.RLock()
		v, ok := p.m[key]
		par := p.parent
		p.mu.RUnlock()
		if ok {
			return v, true
		}
		p = par
	}
	return nil, false
}

func (p *UserProfile) GetBool(key string) bool {
	v, ok := p.Get(key)
	if !ok {
		return false
	}
	b, _ := v.(bool)
	return b
}

func (p *UserProfile) GetString(key string) string {
	v, ok := p.Get(key)
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func (p *UserProfile) Has(flag string) bool {
	return p.GetBool(flag)
}

func (p *UserProfile) IsOwner() bool {
	return p.Has(PermOwner)
}

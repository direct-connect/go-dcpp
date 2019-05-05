package hub

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
)

var (
	ErrUserRegDisabled = errors.New("user registration is disabled")
	ErrUserNotFound    = errors.New("user does not exist")
	ErrNameTaken       = errors.New("user name already taken")
)

const (
	userNameMin = 3
	userNameMax = 256
)

type BanKey string

type Ban struct {
	Key    BanKey
	Hard   bool
	Until  time.Time
	Reason string
}

type User struct {
	aname atomic.Value // string

	mu      sync.RWMutex
	ban     *Ban
	profile *UserProfile
}

func (u *User) Name() string {
	name, _ := u.aname.Load().(string)
	return name
}

func (u *User) setName(name string) {
	u.aname.Store(string(name))
}

func (u *User) SetName(name string) {
	u.setName(name)
}

func (u *User) IsBanned() *Ban {
	if u == nil {
		return nil
	}
	u.mu.RLock()
	b := u.ban
	u.mu.RUnlock()
	return b
}

func (u *User) BanUntil(t time.Time, reason string) {
	u.mu.Lock()
	if u.ban != nil && t.Before(u.ban.Until) {
		u.mu.Unlock()
		return
	}
	u.ban = &Ban{Until: t, Reason: reason}
	u.mu.Unlock()
}

func (u *User) Profile() *UserProfile {
	if u == nil {
		return nil
	}
	u.mu.RLock()
	p := u.profile
	u.mu.RUnlock()
	return p
}

func (u *User) SetProfile(p *UserProfile) {
	u.mu.Lock()
	u.profile = p
	u.mu.Unlock()
}

func (u *User) Has(flag string) bool {
	return u.Profile().Has(flag)
}

func (u *User) HasPerm(perm string) bool {
	if perm == "" || u.IsOwner() {
		return true
	}
	return u.Profile().Has(perm)
}

func (u *User) IsOwner() bool {
	return u.Profile().IsOwner()
}

type Database interface {
	UserDatabase
	ProfileDatabase
	BanDatabase
	Close() error
}

type UserRecord struct {
	Name    string
	Pass    string
	Profile string
}

type UserDatabase interface {
	IsRegistered(name string) (bool, error)
	GetUser(name string) (*UserRecord, error)
	CreateUser(rec UserRecord) error
	DeleteUser(name string) error
	ListUsers() ([]UserRecord, error)
	UpdateUser(name string, fnc func(u *UserRecord) (bool, error)) error
}

type Map map[string]interface{}

func (m Map) Clone() Map {
	m2 := make(Map, len(m))
	for k, v := range m {
		if sm, ok := v.(Map); ok {
			v = sm.Clone()
		}
		m2[k] = v
	}
	return m2
}

type ProfileDatabase interface {
	GetProfile(id string) (Map, error)
	PutProfile(id string, m Map) error
	DelProfile(id string) error
	ListProfiles() ([]string, error)
}

type BanDatabase interface {
	ListBans() ([]Ban, error)
	GetBan(key BanKey) (*Ban, error)
	PutBans(bans []Ban) error
	DelBans(keys []BanKey) error
	ClearBans() error
}

var (
	errNameEmpty         = errors.New("name should not be empty")
	errNameTooLong       = errors.New("name is too long")
	errNameTooShort      = errors.New("name is too short")
	errNameInvalidPrefix = errors.New("name start with an invalid character")
	errNamePadding       = errors.New("name should not start or end with spaces")
	errNameInvalidChars  = errors.New("name contains invalid characters (' ', '<', '>', etc)")
)

func (h *Hub) validateUserName(name string) error {
	if name == "" {
		return errNameEmpty
	}
	if len(name) > userNameMax {
		return errNameTooLong
	} else if len(name) < userNameMin {
		return errNameTooShort
	}
	switch name[0] {
	case ' ', '#', '!', '+', '/':
		return errNameInvalidPrefix
	}
	if name != strings.TrimSpace(name) {
		return errNamePadding
	}
	if strings.ContainsAny(name, "\x00 <>") {
		return errNameInvalidChars
	}
	if strings.IndexFunc(name, func(r rune) bool {
		if r < ' ' {
			return true
		}
		if !unicode.IsGraphic(r) {
			return true
		}
		return false
	}) >= 0 {
		return errNameInvalidChars
	}
	if h.fallback != nil {
		_, err := h.fallback.NewEncoder().String(name)
		if err != nil {
			return fmt.Errorf("invalid name: %v (%q)", err, name)
		}
	}
	return nil
}

func (h *Hub) RegisterUser(name, pass string) error {
	if h.db == nil {
		return ErrUserRegDisabled
	}
	return h.db.CreateUser(UserRecord{Name: name, Pass: pass})
}

func (h *Hub) DeleteUser(name string) error {
	if h.db == nil {
		return nil
	}
	return h.db.DeleteUser(name)
}

func (h *Hub) UpdateUser(name string, fnc func(u *UserRecord) (bool, error)) error {
	if h.db == nil {
		return ErrUserRegDisabled
	}
	return h.db.UpdateUser(name, func(u *UserRecord) (bool, error) {
		if u == nil {
			return false, ErrUserNotFound
		}
		ok, err := fnc(u)
		if err != nil {
			return false, nil
		} else if u.Name != name {
			return false, errors.New("user name cannot be changed")
		}
		return ok, err
	})
}

func (h *Hub) IsRegistered(name string) (bool, error) {
	if h.db == nil {
		return false, nil
	}
	return h.db.IsRegistered(name)
}

func (h *Hub) getUser(name string) (*User, *UserRecord, error) {
	if h.db == nil {
		return nil, nil, nil
	}
	// TODO: cache
	rec, err := h.db.GetUser(name)
	if err != nil || rec == nil {
		return nil, nil, err
	}
	u := &User{profile: h.Profile(rec.Profile)}
	if u.profile == nil {
		u.profile = h.Profile(ProfileNameRegistered)
	}
	u.setName(name)
	return u, rec, nil
}

// NewDatabase creates an in-memory users database.
func NewDatabase() Database {
	return &memDB{
		users:    make(map[string]UserRecord),
		profiles: make(map[string]Map),
		bans:     make(map[BanKey]Ban),
	}
}

type memDB struct {
	mu       sync.RWMutex
	users    map[string]UserRecord
	profiles map[string]Map
	bans     map[BanKey]Ban
}

func (*memDB) Close() error {
	return nil
}

func (db *memDB) IsRegistered(name string) (bool, error) {
	db.mu.RLock()
	_, ok := db.users[name]
	db.mu.RUnlock()
	return ok, nil
}

func (db *memDB) GetUser(name string) (*UserRecord, error) {
	db.mu.RLock()
	rec, ok := db.users[name]
	db.mu.RUnlock()
	if !ok {
		return nil, nil
	}
	return &rec, nil
}

func (db *memDB) ListUsers() ([]UserRecord, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	var out []UserRecord
	for _, rec := range db.users {
		out = append(out, rec)
	}
	return out, nil
}

func (db *memDB) CreateUser(rec UserRecord) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.users[rec.Name] = rec
	return nil
}

func (db *memDB) DeleteUser(name string) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	delete(db.users, name)
	return nil
}

func (db *memDB) UpdateUser(name string, fnc func(u *UserRecord) (bool, error)) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	u, ok := db.users[name]
	if !ok {
		_, err := fnc(nil)
		return err
	}
	update, err := fnc(&u)
	if err != nil || !update {
		return err
	}
	db.users[name] = u
	return nil
}

func (db *memDB) GetProfile(id string) (Map, error) {
	db.mu.RLock()
	m, ok := db.profiles[id]
	db.mu.RUnlock()
	if !ok {
		return nil, nil
	}
	return m.Clone(), nil
}

func (db *memDB) PutProfile(id string, m Map) error {
	m = m.Clone()
	db.mu.Lock()
	db.profiles[id] = m
	db.mu.Unlock()
	return nil
}

func (db *memDB) DelProfile(id string) error {
	db.mu.Lock()
	delete(db.profiles, id)
	db.mu.Unlock()
	return nil
}

func (db *memDB) ListProfiles() ([]string, error) {
	db.mu.RLock()
	list := make([]string, 0, len(db.profiles))
	for k := range db.profiles {
		list = append(list, k)
	}
	db.mu.RUnlock()
	return list, nil
}

func (db *memDB) ListBans() ([]Ban, error) {
	db.mu.RLock()
	list := make([]Ban, 0, len(db.bans))
	for _, b := range db.bans {
		list = append(list, b)
	}
	db.mu.RUnlock()
	return list, nil
}

func (db *memDB) GetBan(key BanKey) (*Ban, error) {
	db.mu.RLock()
	b, ok := db.bans[key]
	db.mu.RUnlock()
	if !ok {
		return nil, nil
	}
	return &b, nil
}

func (db *memDB) PutBans(bans []Ban) error {
	db.mu.Lock()
	for _, b := range bans {
		db.bans[b.Key] = b
	}
	db.mu.Unlock()
	return nil
}

func (db *memDB) DelBans(keys []BanKey) error {
	db.mu.Lock()
	for _, k := range keys {
		delete(db.bans, k)
	}
	db.mu.Unlock()
	return nil
}

func (db *memDB) ClearBans() error {
	db.mu.Lock()
	db.bans = make(map[BanKey]Ban)
	db.mu.Unlock()
	return nil
}

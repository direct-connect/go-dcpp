package hubdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"github.com/direct-connect/go-dcpp/hub"

	_ "github.com/hidal-go/hidalgo/kv/all"

	"github.com/hidal-go/hidalgo/kv"
	"github.com/hidal-go/hidalgo/kv/kvdebug"
	"github.com/hidal-go/hidalgo/tuple"
	"github.com/hidal-go/hidalgo/tuple/kv"
	"github.com/hidal-go/hidalgo/values"
)

const (
	debug = false

	tableUsers       = "users"
	tableUsersByName = "usersByName" // TODO: replace with secondary index once it's supported
	tableProfiles    = "profiles"
)

func Open(typ, path string) (hub.Database, error) {
	reg := kv.ByName(typ)
	if reg == nil {
		return nil, fmt.Errorf("unsupported database kind: %q", typ)
	}
	kdb, err := reg.OpenPath(path)
	if err != nil {
		return nil, err
	}
	if debug {
		kdbg := kvdebug.New(kdb)
		kdbg.Log(true)
		kdb = kdbg
	}

	db := &tupleDatabase{db: tuplekv.New(kdb)}
	err = db.openTables()
	if err != nil {
		return nil, fmt.Errorf("cannot open tables: %v", err)
	}
	return db, nil
}

type tupleDatabase struct {
	db          tuple.Store
	users       tuple.TableInfo
	usersByName tuple.TableInfo
	profiles    tuple.TableInfo
}

func (db *tupleDatabase) Close() error {
	return db.db.Close()
}

func (db *tupleDatabase) openTables() error {
	ctx := context.TODO()
	if err := db.openUsers(ctx); err != nil {
		return err
	}
	if err := db.openUsersIndex(ctx); err != nil {
		return err
	}
	if err := db.openProfiles(ctx); err != nil {
		return err
	}
	return nil
}

func (db *tupleDatabase) createTable(ctx context.Context, tx tuple.Tx, h tuple.Header) error {
	_, err := tx.CreateTable(ctx, h)
	if err != nil {
		return fmt.Errorf("cannot create table '%s': %v", h.Name, err)
	}
	return nil
}

func (db *tupleDatabase) upgradeUsers(ctx context.Context) error {
	if h := db.users.Header(); len(h.Key) == 1 && !h.Key[0].Auto {
		// users indexed directly by name
		if err := db.migrateUsersV2(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (db *tupleDatabase) migrateUsersV2(ctx context.Context) error {
	log.Println("migrating users table to v2")
	// read all users and their passwords
	tx, err := db.db.Tx(false)
	if err != nil {
		return err
	}
	defer tx.Close()

	tbl, err := db.users.Open(tx)
	if err != nil {
		return err
	}

	it := tbl.Scan(nil)
	defer it.Close()

	var users [][2]values.String // name, pass
	for it.Next(ctx) {
		name, ok := it.Key()[0].(values.String)
		if !ok {
			return fmt.Errorf("expected string name, got: %T", it.Key()[0])
		}
		pass, ok := it.Data()[0].(values.String)
		if !ok {
			return fmt.Errorf("expected string pass, got: %T", it.Data()[0])
		}
		users = append(users, [2]values.String{name, pass})
	}
	if err := it.Err(); err != nil {
		return err
	}
	_ = it.Close()
	_ = tx.Close()

	// drop an old table, create a new one
	tx, err = db.db.Tx(true)
	if err != nil {
		return err
	}
	defer tx.Close()

	tbl, err = db.users.Open(tx)
	if err != nil {
		return err
	}
	if err = tbl.Drop(ctx); err != nil {
		return err
	}
	if err = db.createUsersV2(ctx, tx); err != nil {
		return err
	}
	if err = db.createUsersIndexV2(ctx, tx); err != nil {
		return err
	}
	tbl, err = tx.Table(ctx, tableUsers)
	if err != nil {
		return err
	}
	index, err := tx.Table(ctx, tableUsersByName)
	if err != nil {
		return err
	}
	for _, u := range users {
		name, pass := u[0], u[1]
		key, err := tbl.InsertTuple(ctx, tuple.Tuple{
			Key:  tuple.AutoKey(),
			Data: tuple.Data{name, pass, nil},
		})
		if err != nil {
			return err
		}
		_, err = index.InsertTuple(ctx, tuple.Tuple{
			Key:  tuple.Key{name},
			Data: tuple.Data{key[0]},
		})
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (db *tupleDatabase) createUsersV2(ctx context.Context, tx tuple.Tx) error {
	return db.createTable(ctx, tx, tuple.Header{
		Name: tableUsers,
		Key: []tuple.KeyField{
			{Name: "id", Type: values.UIntType{}, Auto: true},
		},
		Data: []tuple.Field{
			{Name: "name", Type: values.StringType{}},
			// TODO: unfortunately we have to store it in plain text
			//       due to the protocol limitations
			{Name: "pass", Type: values.StringType{}},
			{Name: "profile", Type: values.StringType{}},
		},
	})
}

func (db *tupleDatabase) createUsersIndexV2(ctx context.Context, tx tuple.Tx) error {
	return db.createTable(ctx, tx, tuple.Header{
		Name: tableUsersByName,
		Key: []tuple.KeyField{
			{Name: "name", Type: values.StringType{}},
		},
		Data: []tuple.Field{
			{Name: "id", Type: values.UIntType{}},
		},
	})
}

func (db *tupleDatabase) createProfilesV2(ctx context.Context, tx tuple.Tx) error {
	return db.createTable(ctx, tx, tuple.Header{
		Name: tableProfiles,
		Key: []tuple.KeyField{
			{Name: "id", Type: values.StringType{}},
		},
		Data: []tuple.Field{
			{Name: "m", Type: values.StringType{}},
		},
	})
}

func (db *tupleDatabase) inTx(ctx context.Context, rw bool, fnc func(ctx context.Context, tx tuple.Tx) error) error {
	tx, err := db.db.Tx(rw)
	if err != nil {
		return err
	}
	defer tx.Close()
	if err = fnc(ctx, tx); err != nil {
		return err
	}
	if !rw {
		return tx.Close()
	}
	return tx.Commit(ctx)
}

func (db *tupleDatabase) openUsers(ctx context.Context) error {
	users, err := db.db.Table(ctx, tableUsers)
	if err == nil {
		db.users = users
		if err = db.upgradeUsers(ctx); err != nil {
			return err
		}
		users, err = db.db.Table(ctx, tableUsers)
		if err != nil {
			return err
		}
		db.users = users
		return nil
	} else if err != tuple.ErrTableNotFound {
		return err
	}
	if err := db.inTx(ctx, true, db.createUsersV2); err != nil {
		return err
	}
	if err := db.inTx(ctx, true, db.createUsersIndexV2); err != nil {
		return err
	}
	users, err = db.db.Table(ctx, tableUsers)
	if err != nil {
		return err
	}
	db.users = users
	return nil
}

func (db *tupleDatabase) openUsersIndex(ctx context.Context) error {
	users, err := db.db.Table(ctx, tableUsersByName)
	if err != nil {
		return err
	}
	db.usersByName = users
	return nil
}

func (db *tupleDatabase) openProfiles(ctx context.Context) error {
	prof, err := db.db.Table(ctx, tableProfiles)
	if err == nil {
		db.profiles = prof
		return nil
	} else if err != tuple.ErrTableNotFound {
		return err
	}
	if err := db.inTx(ctx, true, db.createProfilesV2); err != nil {
		return err
	}
	prof, err = db.db.Table(ctx, tableProfiles)
	if err != nil {
		return err
	}
	db.profiles = prof
	return nil
}

func (db *tupleDatabase) lookupUser(ctx context.Context, tx tuple.Tx, name string) (tuple.Key, error) {
	index, err := db.usersByName.Open(tx)
	if err != nil {
		return nil, err
	}
	data, err := index.GetTuple(ctx, tuple.SKey(name))
	if err != nil {
		return nil, err
	}
	id, ok := data[0].(tuple.Sortable)
	if !ok {
		return nil, errors.New("invalid user name index")
	}
	return tuple.Key{id}, nil
}

func (db *tupleDatabase) IsRegistered(name string) (bool, error) {
	tx, err := db.db.Tx(false)
	if err != nil {
		return false, err
	}
	defer tx.Close()
	_, err = db.lookupUser(context.TODO(), tx, name)
	if err == tuple.ErrNotFound {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

func asUserRec(data tuple.Data) (*hub.UserRecord, error) {
	rname, ok := data[0].(values.String)
	if !ok {
		return nil, fmt.Errorf("expected string name, got: %T", data[0])
	}
	pass, ok := data[1].(values.String)
	if !ok {
		return nil, fmt.Errorf("expected string pass, got: %T", data[1])
	}
	prof, ok := data[2].(values.String)
	if !ok {
		return nil, fmt.Errorf("expected string pass, got: %T", data[2])
	}
	return &hub.UserRecord{
		Name:    string(rname),
		Pass:    string(pass),
		Profile: string(prof),
	}, nil
}

func fromUserRec(u *hub.UserRecord) tuple.Data {
	return tuple.SData(u.Name, u.Pass, u.Profile)
}

func (db *tupleDatabase) GetUser(name string) (*hub.UserRecord, error) {
	tx, err := db.db.Tx(false)
	if err != nil {
		return nil, err
	}
	defer tx.Close()

	ctx := context.TODO()
	key, err := db.lookupUser(ctx, tx, name)
	if err == tuple.ErrNotFound {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	users, err := db.users.Open(tx)
	if err != nil {
		return nil, err
	}
	data, err := users.GetTuple(ctx, key)
	if err == tuple.ErrNotFound {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	rec, err := asUserRec(data)
	if err != nil {
		return nil, err
	} else if name != rec.Name {
		return nil, errors.New("inconsistent user name index")
	}
	return rec, nil
}

func (db *tupleDatabase) ListUsers() ([]hub.UserRecord, error) {
	tx, err := db.db.Tx(false)
	if err != nil {
		return nil, err
	}
	defer tx.Close()

	ctx := context.TODO()
	users, err := db.users.Open(tx)
	if err != nil {
		return nil, err
	}
	it := users.Scan(nil)
	defer it.Close()

	var out []hub.UserRecord
	for it.Next(ctx) {
		data := it.Data()
		if len(data) != 3 {
			if err = it.Err(); err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("invalid users table format (%d)", len(data))
		}
		rec, err := asUserRec(it.Data())
		if err != nil {
			return nil, err
		}
		out = append(out, *rec)
	}
	return out, it.Err()
}

func (db *tupleDatabase) CreateUser(u hub.UserRecord) error {
	tx, err := db.db.Tx(true)
	if err != nil {
		return err
	}
	defer tx.Close()

	ctx := context.TODO()
	key, err := db.lookupUser(ctx, tx, u.Name)
	if err == nil {
		return hub.ErrNameTaken
	} else if err != tuple.ErrNotFound {
		return err
	}

	users, err := db.users.Open(tx)
	if err != nil {
		return err
	}
	key, err = users.InsertTuple(ctx, tuple.Tuple{
		Key:  tuple.AutoKey(),
		Data: fromUserRec(&u),
	})
	if err != nil {
		return err
	}

	index, err := db.usersByName.Open(tx)
	if err != nil {
		return err
	}
	_, err = index.InsertTuple(ctx, tuple.Tuple{
		Key:  tuple.SKey(u.Name),
		Data: tuple.Data{key[0]},
	})
	if err == tuple.ErrExists {
		return hub.ErrNameTaken
	} else if err != nil {
		return err
	}
	err = tx.Commit(ctx)
	if err != nil {
		return err
	}
	return nil
}

func (db *tupleDatabase) DeleteUser(name string) error {
	tx, err := db.db.Tx(true)
	if err != nil {
		return err
	}
	defer tx.Close()

	ctx := context.TODO()
	key, err := db.lookupUser(ctx, tx, name)
	if err == tuple.ErrNotFound {
		return nil
	} else if err != nil {
		return err
	}

	index, err := db.usersByName.Open(tx)
	if err != nil {
		return err
	}
	err = index.DeleteTuples(ctx, &tuple.Filter{
		KeyFilter: tuple.Keys{tuple.SKey(name)},
	})
	if err != nil {
		return err
	}

	users, err := db.users.Open(tx)
	if err != nil {
		return err
	}
	err = users.DeleteTuples(ctx, &tuple.Filter{
		KeyFilter: tuple.Keys{key},
	})
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (db *tupleDatabase) UpdateUser(name string, fnc func(u *hub.UserRecord) (bool, error)) error {
	tx, err := db.db.Tx(true)
	if err != nil {
		return err
	}
	defer tx.Close()

	ctx := context.TODO()
	key, err := db.lookupUser(ctx, tx, name)
	if err == tuple.ErrNotFound {
		_, err = fnc(nil)
		return err
	} else if err != nil {
		return err
	}

	users, err := db.users.Open(tx)
	if err != nil {
		return err
	}
	data, err := users.GetTuple(ctx, key)
	if err == tuple.ErrNotFound {
		_, err = fnc(nil)
		return err
	} else if err != nil {
		return err
	}
	rec, err := asUserRec(data)
	if err != nil {
		return err
	}
	ok, err := fnc(rec)
	if err != nil || !ok {
		return err
	}

	err = users.UpdateTuple(ctx, tuple.Tuple{
		Key: key, Data: fromUserRec(rec),
	}, nil)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (db *tupleDatabase) GetProfile(id string) (hub.Map, error) {
	tx, err := db.db.Tx(false)
	if err != nil {
		return nil, err
	}
	defer tx.Close()

	tbl, err := db.profiles.Open(tx)
	if err != nil {
		return nil, err
	}

	data, err := tbl.GetTuple(context.TODO(), tuple.SKey(id))
	if err == tuple.ErrNotFound {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	s, ok := data[0].(values.String)
	if !ok {
		return nil, fmt.Errorf("expected string profile data, got: %T", data[0])
	}
	m := make(hub.Map)
	err = json.Unmarshal([]byte(s), m)
	if err != nil {
		return nil, err
	}
	return m, nil
}

func (db *tupleDatabase) PutProfile(id string, m hub.Map) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	tx, err := db.db.Tx(true)
	if err != nil {
		return err
	}
	defer tx.Close()

	ctx := context.TODO()
	tbl, err := db.profiles.Open(tx)
	if err != nil {
		return err
	}
	if m == nil {
		m = make(hub.Map)
	}
	err = tbl.UpdateTuple(ctx, tuple.Tuple{
		Key:  tuple.SKey(id),
		Data: tuple.SData(string(data)),
	}, &tuple.UpdateOpt{Upsert: true})
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (db *tupleDatabase) DelProfile(id string) error {
	tx, err := db.db.Tx(true)
	if err != nil {
		return err
	}
	defer tx.Close()

	ctx := context.TODO()
	tbl, err := db.profiles.Open(tx)
	if err != nil {
		return err
	}
	err = tbl.DeleteTuples(ctx, &tuple.Filter{
		KeyFilter: tuple.Keys{tuple.SKey(id)},
	})
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (db *tupleDatabase) ListProfiles() ([]string, error) {
	tx, err := db.db.Tx(false)
	if err != nil {
		return nil, err
	}
	defer tx.Close()

	tbl, err := db.profiles.Open(tx)
	if err != nil {
		return nil, err
	}

	ctx := context.TODO()
	it := tbl.Scan(&tuple.ScanOptions{
		KeysOnly: true,
	})
	var out []string
	for it.Next(ctx) {
		id, ok := it.Key()[0].(values.String)
		if !ok {
			return nil, fmt.Errorf("expected string profile id, got: %T", it.Key()[0])
		}
		out = append(out, string(id))
	}
	if err := it.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

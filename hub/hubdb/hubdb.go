package hubdb

import (
	"context"
	"fmt"

	"github.com/direct-connect/go-dcpp/hub"

	_ "github.com/hidal-go/hidalgo/kv/all"

	"github.com/hidal-go/hidalgo/kv"
	"github.com/hidal-go/hidalgo/tuple"
	"github.com/hidal-go/hidalgo/tuple/kv"
	"github.com/hidal-go/hidalgo/values"
)

const (
	tableUsers = "users"
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
	ctx := context.TODO()
	db := tuplekv.New(kdb)
	users, err := db.Table(ctx, tableUsers)
	if err == tuple.ErrTableNotFound {
		// create table
		var tx tuple.Tx
		tx, err = db.Tx(true)
		if err != nil {
			return nil, fmt.Errorf("cannot create tables: %v", err)
		}
		defer tx.Close()
		_, err = tx.CreateTable(ctx, tuple.Header{
			Name: tableUsers,
			Key: []tuple.KeyField{
				// TODO: add auto-increment field once it's supported
				{Name: "name", Type: values.StringType{}},
			},
			Data: []tuple.Field{
				// TODO: unfortunately we have to store it in plain text
				//       due to the protocol limitations
				{Name: "pass", Type: values.StringType{}},
			},
		})
		if err != nil {
			return nil, fmt.Errorf("cannot create tables: %v", err)
		}
		err = tx.Commit(ctx)
		if err != nil {
			return nil, fmt.Errorf("cannot create tables: %v", err)
		}
		users, err = db.Table(ctx, tableUsers)
	}
	if err != nil {
		return nil, fmt.Errorf("cannot open tables: %v", err)
	}
	return &tupleDatabase{db: db, users: users}, nil
}

type tupleDatabase struct {
	db    tuple.Store
	users tuple.TableInfo
}

func (db *tupleDatabase) IsRegistered(name string) (bool, error) {
	tx, err := db.db.Tx(false)
	if err != nil {
		return false, err
	}
	defer tx.Close()
	users, err := db.users.Open(tx)
	if err != nil {
		return false, err
	}
	_, err = users.GetTuple(context.TODO(), tuple.SKey(name))
	if err == tuple.ErrNotFound {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

func (db *tupleDatabase) GetUserPassword(name string) (string, error) {
	tx, err := db.db.Tx(false)
	if err != nil {
		return "", err
	}
	defer tx.Close()
	users, err := db.users.Open(tx)
	if err != nil {
		return "", err
	}
	user, err := users.GetTuple(context.TODO(), tuple.SKey(name))
	if err == tuple.ErrNotFound {
		return "", nil
	} else if err != nil {
		return "", err
	}
	pass, ok := user[0].(values.String)
	if !ok {
		return "", fmt.Errorf("expected string, got: %T", user[0])
	}
	return string(pass), nil
}

func (db *tupleDatabase) RegisterUser(name, pass string) error {
	tx, err := db.db.Tx(true)
	if err != nil {
		return err
	}
	defer tx.Close()
	users, err := db.users.Open(tx)
	if err != nil {
		return err
	}
	ctx := context.TODO()
	_, err = users.InsertTuple(ctx, tuple.Tuple{
		Key:  tuple.SKey(name),
		Data: tuple.SData(pass),
	})
	if err == nil {
		err = tx.Commit(ctx)
	}
	return err
}

func (db *tupleDatabase) Close() error {
	return db.db.Close()
}

package cmd

import (
	"errors"
	"log"

	"github.com/direct-connect/go-dcpp/hub"
	"github.com/direct-connect/go-dcpp/hub/hubdb"
)

var hubDB hub.Database

func OpenDB() error {
	conf, _, err := readConfig(false)
	if err != nil {
		return err
	} else if conf.Database.Type == "" || conf.Database.Type == "mem" {
		return errors.New("database type should be specified in config")
	}
	log.Printf("using database: %s (%s)\n", conf.Database.Path, conf.Database.Type)
	hubDB, err = hubdb.Open(conf.Database.Type, conf.Database.Path)
	if err != nil {
		return err
	}
	return nil
}

func CloseDB() error {
	if hubDB == nil {
		return nil
	}
	return hubDB.Close()
}

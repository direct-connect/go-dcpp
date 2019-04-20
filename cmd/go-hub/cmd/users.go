package cmd

import (
	"errors"
	"fmt"
	"log"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/direct-connect/go-dcpp/hub"
	"github.com/direct-connect/go-dcpp/hub/hubdb"
)

func init() {
	var db hub.Database
	cmdUsers := &cobra.Command{
		Use:     "users [command]",
		Aliases: []string{"user", "u"},
		Short:   "user-related commands",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			conf, err := readConfig(false)
			if err != nil {
				return err
			} else if conf.Database.Type == "" || conf.Database.Type == "mem" {
				return errors.New("database type should be specified in config")
			}
			log.Printf("using database: %s (%s)\n", conf.Database.Path, conf.Database.Type)
			db, err = hubdb.Open(conf.Database.Type, conf.Database.Path)
			if err != nil {
				return err
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			list, err := db.ListUsers()
			if err != nil {
				return err
			}
			fmt.Println("NAME\t\tPROFILE")
			for _, u := range list {
				q := strconv.Quote(u.Name)
				if q == `"`+u.Name+`"` {
					q = u.Name
				}
				fmt.Printf("%s\t\t%s\n", q, u.Profile)
			}
			return nil
		},
		PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
			if db == nil {
				return nil
			}
			return db.Close()
		},
	}
	Root.AddCommand(cmdUsers)

	cmdList := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "list registered users",
		RunE:    cmdUsers.RunE,
	}
	cmdUsers.AddCommand(cmdList)

	cmdAdd := &cobra.Command{
		Use:     "create <name> <pass> [profile]",
		Aliases: []string{"add", "reg", "register"},
		Short:   "registers a new user",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 2 || len(args) > 3 {
				return errors.New("expected 2 or 3 arguments")
			}
			name, pass := args[0], args[1]
			if name == "" {
				return errors.New("name should not be empty")
			} else if pass == "" {
				return errors.New("password should not be empty")
			}
			profile := ""
			if len(args) > 2 {
				profile = args[2]
			}
			if profile != "" {
				def := hub.DefaultProfiles()
				if _, ok := def[profile]; !ok {
					m, err := db.GetProfile(profile)
					if err != nil {
						return err
					} else if m == nil {
						return errors.New("profile does not exist")
					}
				}
			}
			err := db.CreateUser(hub.UserRecord{
				Name: name, Pass: pass, Profile: profile,
			})
			if err != nil {
				return err
			}
			return nil
		},
	}
	cmdUsers.AddCommand(cmdAdd)

	cmdPromote := &cobra.Command{
		Use:     "promote <name> <profile>",
		Aliases: []string{"up"},
		Short:   "set a profile for a given user",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 2 {
				return errors.New("expected 2 arguments")
			}
			name, profile := args[0], args[1]
			if name == "" {
				return errors.New("name should not be empty")
			} else if profile == "" {
				return errors.New("profile should not be empty")
			}
			def := hub.DefaultProfiles()
			if _, ok := def[profile]; !ok {
				m, err := db.GetProfile(profile)
				if err != nil {
					return err
				} else if m == nil {
					return errors.New("profile does not exist")
				}
			}
			return db.UpdateUser(name, func(u *hub.UserRecord) (bool, error) {
				if u == nil {
					return false, errors.New("user does not exist")
				}
				if u.Profile == profile {
					return false, nil
				}
				u.Profile = profile
				return true, nil
			})
		},
	}
	cmdUsers.AddCommand(cmdPromote)

	cmdDel := &cobra.Command{
		Use:     "delete <name>",
		Aliases: []string{"del", "rm"},
		Short:   "delete user",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errors.New("expected user name")
			}
			name := args[0]
			if name == "" {
				return errors.New("name should not be empty")
			}
			return db.DeleteUser(name)
		},
	}
	cmdUsers.AddCommand(cmdDel)
}

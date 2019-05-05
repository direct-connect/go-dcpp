package cmd

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/direct-connect/go-dcpp/hub"
)

func init() {
	cmdUsers := &cobra.Command{
		Use:     "users [command]",
		Aliases: []string{"user", "u"},
		Short:   "user-related commands",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return OpenDB()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			list, err := hubDB.ListUsers()
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
			return CloseDB()
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
					m, err := hubDB.GetProfile(profile)
					if err != nil {
						return err
					} else if m == nil {
						return errors.New("profile does not exist")
					}
				}
			}
			err := hubDB.CreateUser(hub.UserRecord{
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
				m, err := hubDB.GetProfile(profile)
				if err != nil {
					return err
				} else if m == nil {
					return errors.New("profile does not exist")
				}
			}
			return hubDB.UpdateUser(name, func(u *hub.UserRecord) (bool, error) {
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
			return hubDB.DeleteUser(name)
		},
	}
	cmdUsers.AddCommand(cmdDel)
}

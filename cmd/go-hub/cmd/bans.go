package cmd

import (
	"errors"
	"fmt"
	"net"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/direct-connect/go-dcpp/hub"
)

func init() {
	cmdBans := &cobra.Command{
		Use:     "bans [command]",
		Aliases: []string{"ban"},
		Short:   "ban-related commands",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return OpenDB()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			list, err := hubDB.ListBans()
			if err != nil {
				return err
			}
			for _, b := range list {
				var s string
				if ip := b.Key.ToIP(); ip != nil {
					s = ip.String()
				} else {
					s = strconv.Quote(string(b.Key))
					if s == `"`+string(b.Key)+`"` {
						s = string(b.Key)
					}
				}
				fmt.Printf("%s\n", s)
			}
			return nil
		},
		PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
			return CloseDB()
		},
	}
	Root.AddCommand(cmdBans)

	cmdList := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "list active bans",
		RunE:    cmdBans.RunE,
	}
	cmdBans.AddCommand(cmdList)

	cmdBanIP := &cobra.Command{
		Use:     "ip <ip> [ip ...]",
		Aliases: []string{"add"},
		Short:   "add an IP to a ban list",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("expected at least one IP")
			}
			var bans []hub.Ban
			for _, s := range args {
				ip := net.ParseIP(s)
				if ip == nil {
					return fmt.Errorf("invalid IP format: %q", s)
				}
				bans = append(bans, hub.Ban{Key: hub.MinIPKey(ip), Hard: true})
			}
			return hubDB.PutBans(bans)
		},
	}
	cmdBans.AddCommand(cmdBanIP)

	cmdUnbanIP := &cobra.Command{
		Use:     "unban <ip> [ip ...]",
		Aliases: []string{"un", "del", "rm"},
		Short:   "remove an IP from a ban list",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("expected at least one IP")
			}
			var bans []hub.BanKey
			for _, s := range args {
				ip := net.ParseIP(s)
				if ip == nil {
					return fmt.Errorf("invalid IP format: %q", s)
				}
				bans = append(bans, hub.MinIPKey(ip))
			}
			return hubDB.DelBans(bans)
		},
	}
	cmdBans.AddCommand(cmdUnbanIP)

	cmdClear := &cobra.Command{
		Use:   "clear",
		Short: "clear ban list",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return errors.New("expected no arguments")
			}
			return hubDB.ClearBans()
		},
	}
	cmdBans.AddCommand(cmdClear)
}

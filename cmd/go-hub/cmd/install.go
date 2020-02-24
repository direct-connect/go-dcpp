package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func init() {
	cmdInstall := &cobra.Command{
		Use:   "install",
		Short: "install the hub as a system service",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			wd, err := os.Getwd()
			if err != nil {
				return err
			}
			wd, err = filepath.Abs(wd)
			if err != nil {
				return err
			}
			fmt.Println("installing hub to:", wd)
			bin, err := filepath.Abs(os.Args[0])
			if err != nil {
				return err
			}
			return installService(wd, bin)
		},
	}
	Root.AddCommand(cmdInstall)
}

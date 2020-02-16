//+build linux darwin

package cmd

import (
	"log"
	"syscall"
)

func setLimits() error {
	var limits syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &limits); err != nil {
		return err
	}
	limits.Cur = limits.Max
	log.Println("setting FDs limit to", limits.Max)
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &limits); err != nil {
		return err
	}
	return nil
}

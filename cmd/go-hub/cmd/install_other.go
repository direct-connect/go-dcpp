//+build !linux

package cmd

import (
	"fmt"
	"runtime"
)

func installService(path, bin string) error {
	return fmt.Errorf("automitic install is not supported on %s", runtime.GOOS)
}

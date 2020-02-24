//+build linux

package cmd

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
)

func installService(path, bin string) error {
	if _, err := os.Stat(systemdPath); err == nil {
		return systemdInstall(path, bin)
	}
	return errors.New("unsupported init system")
}

const systemdPath = "/etc/systemd"

func systemdInstall(path, bin string) error {
	const (
		name = "gohub"
		typ  = "system"
	)
	conf := fmt.Sprintf(`[Unit]
Description=GoHub
Wants=basic.target
After=basic.target network.target

[Service]
SyslogIdentifier=gohub
StandardOutput=syslog
StandardError=syslog
ExecStart=%s serve
WorkingDirectory=%s
Restart=always

[Install]
WantedBy=multi-user.target`,
		bin, path,
	)
	err := ioutil.WriteFile(filepath.Join(systemdPath, typ, name+".service"), []byte(conf), 0644)
	if err != nil {
		return fmt.Errorf("cannot write the service file: %v", err)
	}
	cmd := exec.Command("systemctl", "enable", name)
	cmd.Stderr = os.Stderr
	if err = cmd.Run(); err != nil {
		return fmt.Errorf("cannot enable the service: %v", err)
	}
	cmd = exec.Command("systemctl", "start", name)
	cmd.Stderr = os.Stderr
	if err = cmd.Run(); err != nil {
		return fmt.Errorf("cannot enable the service: %v", err)
	}
	fmt.Println("done")
	return nil
}

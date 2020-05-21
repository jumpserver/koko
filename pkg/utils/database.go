package utils

import (
	"os/exec"
	"os/user"
)

func IsInstalledMysqlClient() bool {
	if mysqlPath, err := exec.LookPath("mysql"); err == nil {
		cmd := exec.Command(mysqlPath, "-V")
		if err = cmd.Start(); err == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return true
		}
	}
	return false
}

func IsUserExist(username string) bool {
	if _, err := user.Lookup(username); err == nil {
		return true
	}
	return false
}

package utils

import (
	"os/exec"
	"os/user"
)

func IsInstalledMysqlClient() bool {
	if mysqlPath, err := exec.LookPath("mysql"); err == nil {
		cmd := exec.Command(mysqlPath, "-V")
		if err = cmd.Run(); err == nil {
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

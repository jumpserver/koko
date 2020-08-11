package utils

import (
	"bytes"
	"encoding/json"
	"os/exec"
)

func IsInstalledMysqlClient() bool {
	checkLine := "mysql -V"
	cmd := exec.Command("bash", "-c", checkLine)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	if bytes.HasPrefix(out, []byte("mysql")) {
		return true
	}
	return false
}

func IsInstalledKubectlClient() bool {
	checkLine := "kubectl version --client -o json"
	cmd := exec.Command("bash", "-c", checkLine)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	var result map[string]interface{}
	err = json.Unmarshal(out, &result)
	if err != nil {
		return false
	}
	if _, ok := result["clientVersion"]; ok {
		return true
	}
	return false
}

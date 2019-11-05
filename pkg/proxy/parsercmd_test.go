package proxy

import (
	"strings"
	"testing"

	"github.com/jumpserver/koko/pkg/utils"
)

func TestCmdParser_Parse(t *testing.T) {
	var b = []byte("ifconfig \x08\x1b[K\x08\x1b[K\x08\x1b[K\x08\x1b[K\x08\x1b[K\x08\x1b[Konfig")
	data := utils.ParseTerminalData(b)

	if strings.Join(data, "") != "ifconfig" {
		t.Error("data should be ifconfig but not", data)
	}

	b = []byte("ifconfig\xe4\xbd\xa0")
	data = utils.ParseTerminalData(b)
	t.Log("line: ", strings.Join(data, ""))

}

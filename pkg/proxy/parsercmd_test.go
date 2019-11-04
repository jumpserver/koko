package proxy

import (
	"github.com/jumpserver/koko/pkg/utils"
	"strings"
	"testing"
)

func TestCmdParser_Parse(t *testing.T) {
	//p := NewCmdParser("", "")
	//var b = []byte("ifconfig \x08\x1b[K\x08\x1b[K\x08\x1b[K\x08\x1b[K\x08\x1b[K\x08\x1b[Konfig")
	////data := p.Parse(b)
	//if data != "ifconfig" {
	//	t.Error("data should be ifconfig but not")
	//}
	//b = []byte("ifconfig\xe4\xbd\xa0")
	//data = p.Parse(b)
	//fmt.Println("line: ", data)
}

func TestCmdParser(t *testing.T) {
	var b = []byte("ifconfig \x08\x1b[K\x08\x1b[K\x08\x1b[K\x08\x1b[K\x08\x1b[K\x08\x1b[Konfig")
	line := utils.ParseTerminalData(b)
	t.Log("line: ", strings.Join(line, ""))
}

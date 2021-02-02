package proxy

import (
	"strings"
	"testing"

	"github.com/LeeEirc/terminalparser"
)

func TestCmdParser_Parse(t *testing.T) {
	var b = []byte("ifconfig \x08\x1b[K\x08\x1b[K\x08\x1b[K\x08\x1b[K\x08\x1b[K\x08\x1b[Konfig")
	s := terminalparser.Screen{
		Rows:   make([]*terminalparser.Row, 0, 1024),
		Cursor: &terminalparser.Cursor{},
	}
	data := s.Parse(b)
	if strings.Join(data, "") != "ifconfig" {
		t.Error("data should be ifconfig but not", data)
	}

	b = []byte("ifconfig\xe4\xbd\xa0")
	s = terminalparser.Screen{
		Rows:   make([]*terminalparser.Row, 0, 1024),
		Cursor: &terminalparser.Cursor{},
	}
	data = s.Parse(b)
	t.Log("line: ", strings.Join(data, ""))

}

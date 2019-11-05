package proxy

import (
	"testing"
	"time"
)

func TestCmdParser_Parse(t *testing.T) {
	p := NewCmdParser("0000", "test")
	defer p.Close()
	var b = []byte("ifconfig \x08\x1b[K\x08\x1b[K\x08\x1b[K\x08\x1b[K\x08\x1b[K\x08\x1b[Konfig")
	p.WriteData(b)
	time.Sleep(time.Second)
	data := p.Parse()

	if data != "ifconfig" {
		t.Error("data should be ifconfig but not", data)
	}

	b = []byte("ifconfig\xe4\xbd\xa0")
	p.WriteData(b)
	data = p.Parse()
	t.Log("line: ", data)

}

package telnetlib

import (
	"bytes"
)

type packet struct {
	optionCode  byte
	commandCode byte
	subOption   *subOption
}

func (p *packet) generatePacket() []byte {
	var buf bytes.Buffer
	buf.WriteByte(IAC)
	buf.WriteByte(p.optionCode)
	buf.WriteByte(p.commandCode)
	if p.subOption != nil {
		buf.Write(p.subOption.subPacket())
		buf.WriteByte(IAC)
		buf.WriteByte(SE)
	}
	return buf.Bytes()
}

type subOption struct {
	subCommand byte
	options    []byte
}

func (s *subOption) subPacket() []byte {
	if s.subCommand == IAC {
		return s.options
	}
	cp := make([]byte, len(s.options)+1)
	copy(cp, []byte{s.subCommand})
	copy(cp[1:], s.options)
	return cp
}

package zmodem

import (
	"bytes"
	"strconv"
	"time"

	"github.com/jumpserver/koko/pkg/logger"
)

/*

Binary16 Header:

ZDLE A TYPE F3/P0 F2/P1 F1/P2 F0/P3 CRC-1 CRC-2

Binary32 Header
ZDLE C TYPE F3/P0 F2/P1	F1/P2 F0/P3 CRC-1 CRC-2	CRC-3 CRC-4

*/

var (
	zSessionEnd = []byte{0x4f, 0x4f}
)

const (
	bin16HeaderLen = 7
	bin32HeaderLen = 9
)

func DecodeHexFrameHeader(p []byte) (h ZmodemHeader, offset int, ok bool) {
	endPos := bytes.IndexByte(p, 0x8a)
	if endPos == -1 {
		endPos = bytes.IndexByte(p, 0x0a)
	}
	if endPos == -1 {
		return
	}

	hexBytes := p[:endPos]
	hexBytes = bytes.TrimSpace(hexBytes)
	hexBytes = bytes.TrimSuffix(hexBytes, []byte{0x8d})
	if len(hexBytes) != 18 {
		return
	}
	offset = endPos
	hexBytes = hexBytes[4:]
	octets := ConvertHexToOctets(hexBytes)
	hd := ParseNonZDLEBinary16(octets)
	if hd == nil {
		return
	}
	return *hd, offset, true
}

func DecodeB16FrameHeader(p []byte) (h ZmodemHeader, offset int, ok bool) {
	index := bytes.Index(p, Binary16HeaderPrefix)
	remain := p[index+len(Binary16HeaderPrefix):]
	offset = index + len(Binary16HeaderPrefix)
	header := make([]byte, 0, bin16HeaderLen)
	var gotZDLE bool
	for i, value := range remain {
		switch value {
		case ZDLE:
			gotZDLE = true
			continue
		default:
			if gotZDLE {
				header = append(header, value^0x40)
			} else {
				header = append(header, value)
			}
			gotZDLE = false
		}

		if len(header) == bin16HeaderLen {
			offset += i + 1
			break
		}
	}
	if len(header) != bin16HeaderLen {
		return ZmodemHeader{}, offset, false
	}
	// todo crc16 validate data ?
	return ZmodemHeader{
		Type: header[0],
		ZF0:  header[1],
		ZF1:  header[2],
		ZF2:  header[3],
		ZF3:  header[4],
	}, offset, true

}

func DecodeB32FrameHeader(p []byte) (h ZmodemHeader, offset int, ok bool) {
	index := bytes.Index(p, Binary32HeaderPrefix)
	remain := p[index+len(Binary32HeaderPrefix):]
	offset = index + len(Binary32HeaderPrefix)
	header := make([]byte, 0, bin32HeaderLen)
	var gotZDLE bool
	for i, value := range remain {
		switch value {
		case ZDLE:
			gotZDLE = true
			continue
		default:
			if gotZDLE {
				header = append(header, value^0x40)
			} else {
				header = append(header, value)
			}
			gotZDLE = false
		}

		if len(header) == bin32HeaderLen {
			offset += i + 1
			break
		}
	}
	if len(header) != bin32HeaderLen {
		return ZmodemHeader{}, offset, false
	}
	// todo crc32 validate data ?
	return ZmodemHeader{
		Type: header[0],
		ZF0:  header[1],
		ZF1:  header[2],
		ZF2:  header[3],
		ZF3:  header[4],
	}, offset, true
}

type ZmodemHeader struct {
	Type byte
	ZF0  byte
	ZF1  byte
	ZF2  byte
	ZF3  byte
}

const (
	TypeUpload   = "upload"
	TypeDownload = "download"
)

const (
	TransferStatusStart    = "start"
	TransferStatusFinished = "end"
	TransferStatusAbort    = "abort"
)

type ZFileInfo struct {
	filename string
	size     int

	parserTime   time.Time
	transferType string
}

func (z *ZFileInfo) Type() string {
	return z.transferType
}

func (z *ZFileInfo) Filename() string {
	return z.filename
}

func (z *ZFileInfo) Time() time.Time {
	return z.parserTime
}

type ZSession struct {
	Type        string
	endCallback func()
	zFileInfo   *ZFileInfo

	transferStatus string

	subPacketBuf bytes.Buffer

	parsedSubPacket []byte
	haveEnd         bool
	currentHd       *ZmodemHeader

	ZFileHeaderCallback func(zInfo *ZFileInfo)

	zOnHeader func(hd *ZmodemHeader)
}

// zsession 入口
func (s *ZSession) consume(p []byte) {
	if s.gotZFin() {
		s.haveEnd = true
		if bytes.Index(p, zSessionEnd) == 0 {
			if s.endCallback != nil {
				s.endCallback()
			}
			logger.Errorf("Zmodem session %s normally end", s.Type)
			return
		}
		logger.Infof("Zmodem session %s abnormally finish", s.Type)
		return
	}
	if s.checkAbort(p) {
		logger.Infof("Zmodem session %s abort", s.Type)
		s.transferStatus = TransferStatusAbort
		return
	}
	if s.IsNeedSubPacket() {
		s.subPacketBuf.Write(p)
		s.consumeSubPacket()
		return
	}
	s.consumeHeader(p)
	s.consumeSubPacket()
}

func (s *ZSession) checkAbort(p []byte) bool {
	return bytes.Contains(p, AbortSession)
}

func (s *ZSession) consumeHeader(p []byte) {
	if !bytes.Contains(p, []byte{ZPAD, ZDLE}) {
		return
	}
	hexIndex := bytes.Index(p, HexHeaderPrefix)
	if hexIndex != -1 {
		s.getHexHeader(p[hexIndex:])
		return
	}
	b16Index := bytes.Index(p, Binary16HeaderPrefix)
	if b16Index != -1 {
		s.getB16Header(p[b16Index:])
		return
	}
	b32Index := bytes.Index(p, Binary32HeaderPrefix)
	if b32Index != -1 {
		s.getB32Header(p[b32Index:])
	}
}

func (s *ZSession) consumeSubPacket() {
	buf := s.subPacketBuf.Bytes()
	if len(buf) == 0 {
		return
	}
	var (
		offset       int
		gotZDLE      bool
		endSubPacket bool
	)
	for i := range buf {
		switch buf[i] {
		case ZDLE:
			gotZDLE = true
			continue
		case ZCRCE, ZCRCG, ZCRCQ, ZCRCW:
			if gotZDLE {
				endSubPacket = true
			} else {
				s.parsedSubPacket = append(s.parsedSubPacket, buf[i])
			}
		case 0x91, 0x13, 0x11:
			gotZDLE = false
			continue
		default:
			if gotZDLE {
				s.parsedSubPacket = append(s.parsedSubPacket, buf[i]^0x40)
			} else {
				s.parsedSubPacket = append(s.parsedSubPacket, buf[i])
			}
		}
		gotZDLE = false
		if endSubPacket {
			offset = i
			break
		}
	}
	s.subPacketBuf.Reset()
	s.onSubPacket(s.parsedSubPacket)
	s.parsedSubPacket = nil
	s.consume(buf[offset+1:])
}

func (s *ZSession) onSubPacket(p []byte) {
	switch s.currentHd.Type {
	case ZFILE:
		nonZDELData := p
		var info ZFileInfo
		filenameIndex := bytes.IndexByte(nonZDELData, 0x00)
		if filenameIndex == -1 {
			logger.Errorf("解析rz sz 文件名出错 %s", p)
			break
		}
		info.filename = string(nonZDELData[:filenameIndex])
		info.parserTime = time.Now()
		info.transferType = s.Type
		remain := nonZDELData[filenameIndex+1:]
		zFileOptions := bytes.Split(remain, []byte{0x20})
		if len(zFileOptions) >= 1 {
			if size, err := strconv.Atoi(string(zFileOptions[0])); err == nil {
				info.size = size
			} else {
				logger.Errorf("解析rz sz 文件名大小出错 %s", zFileOptions[0])
			}
		}
		s.zFileInfo = &info
		if s.ZFileHeaderCallback != nil {
			s.ZFileHeaderCallback(&info)
		}
	}
	s.currentHd = nil
}

func (s *ZSession) getHexHeader(p []byte) {
	if hd, offset, ok := DecodeHexFrameHeader(p); ok {
		s.onHeader(&hd)
		if s.IsNeedSubPacket() {
			s.subPacketBuf.Write(p[offset:])
		}
	}
}

func (s *ZSession) getB16Header(p []byte) {
	if hd, offset, ok := DecodeB16FrameHeader(p); ok {
		s.onHeader(&hd)
		if s.IsNeedSubPacket() {
			s.subPacketBuf.Write(p[offset:])
		}
	}
}

func (s *ZSession) getB32Header(p []byte) {
	if hd, offset, ok := DecodeB32FrameHeader(p); ok {
		s.onHeader(&hd)
		if s.IsNeedSubPacket() {
			s.subPacketBuf.Write(p[offset:])
		}
	}
}

func (s *ZSession) onHeader(hd *ZmodemHeader) {
	switch hd.Type {
	case ZFILE:
		s.transferStatus = TransferStatusStart
	case ZEOF:
		s.transferStatus = TransferStatusFinished
		s.zFileInfo = nil
	case ZFIN:
		s.haveEnd = true
		if s.endCallback != nil {
			s.endCallback()
		}

	}
	if s.zOnHeader != nil {
		s.zOnHeader(hd)
	}
	s.currentHd = hd
	s.subPacketBuf.Reset()
	logger.Debugf("Zmodem Session type: %s receive header type: %s", s.Type, FrameType(hd.Type))
}

func (s *ZSession) IsEnd() bool {
	return s.haveEnd || s.transferStatus == TransferStatusAbort
}

func (s *ZSession) IsNeedSubPacket() bool {
	return s.currentHd != nil && s.currentHd.Type == ZFILE
}

func (s *ZSession) gotZFin() bool {
	return s.currentHd != nil && s.currentHd.Type == ZFIN
}

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

	sessionHeaderBufferLimit = 256
	maxZFileSubPacketSize    = 64 * 1024
	maxZFileNameSize         = 4 * 1024

	crc16Size = 2
	crc32Size = 4
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

func (z *ZFileInfo) Size() int64 {
	return int64(z.size)
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

	headerBuf        []byte
	parsedSubPacket  []byte
	subPacketGotZDLE bool
	frameCRCSize     int
	dataGotZDLE      bool
	dataCRCRemaining int
	dataFrameEnds    bool
	zfinBuf          []byte
	abortSequenceLen int
	haveEnd          bool
	currentHd        *ZmodemHeader

	ZFileHeaderCallback func(zInfo *ZFileInfo)

	zOnHeader func(hd *ZmodemHeader)

	AbnormalFinish bool
}

// zsession 入口
func (s *ZSession) consume(p []byte) {
	if s.gotZFin() {
		for len(p) > 0 {
			switch p[0] {
			case 0x8a, 0x8d, 0x0a, 0x0d, 0x11, 0x13:
				p = p[1:]
			default:
				s.zfinBuf = append(s.zfinBuf, p...)
				p = nil
			}
		}
		if len(s.zfinBuf) < len(zSessionEnd) {
			return
		}
		s.haveEnd = true
		if bytes.HasPrefix(s.zfinBuf, zSessionEnd) {
			if s.endCallback != nil {
				s.endCallback()
			}
			logger.Errorf("Zmodem session %s normally end", s.Type)
			return
		}
		logger.Infof("Zmodem session %s abnormally finish", s.Type)
		s.AbnormalFinish = true
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
	if s.IsDataPacket() {
		s.consumeDataPacket(p)
		return
	}
	s.consumeHeader(p)
	s.consumeSubPacket()
}

func (s *ZSession) checkAbort(p []byte) bool {
	for _, value := range p {
		if value == CAN {
			s.abortSequenceLen++
			if s.abortSequenceLen >= len(AbortSession) {
				return true
			}
		} else {
			s.abortSequenceLen = 0
		}
	}
	return false
}

func (s *ZSession) consumeHeader(p []byte) {
	s.headerBuf = append(s.headerBuf, p...)

	for len(s.headerBuf) > 0 {
		index, headerType := findFrameHeader(s.headerBuf)
		if index == -1 {
			s.retainHeaderPrefix()
			return
		}

		candidate := s.headerBuf[index:]
		var (
			hd        ZmodemHeader
			offset    int
			ok        bool
			remainPos int
		)
		switch headerType {
		case ZHEX:
			hd, offset, ok = DecodeHexFrameHeader(candidate)
			remainPos = offset + 1
			s.frameCRCSize = crc16Size
		case ZBIN:
			hd, offset, ok = DecodeB16FrameHeader(candidate)
			remainPos = offset
			s.frameCRCSize = crc16Size
		case ZBIN32:
			hd, offset, ok = DecodeB32FrameHeader(candidate)
			remainPos = offset
			s.frameCRCSize = crc32Size
		}
		if !ok {
			// TCP/WebSocket 均可能把一个 ZMODEM 头拆成多个数据包，保留后继续解析。
			hasInvalidTerminator := headerType == ZHEX &&
				(bytes.IndexByte(candidate, 0x8a) != -1 || bytes.IndexByte(candidate, 0x0a) != -1)
			if hasInvalidTerminator || len(candidate) > sessionHeaderBufferLimit {
				s.headerBuf = append(s.headerBuf[:0], candidate[1:]...)
				continue
			}
			s.headerBuf = append(s.headerBuf[:0], candidate...)
			return
		}

		remain := append([]byte(nil), candidate[remainPos:]...)
		s.headerBuf = s.headerBuf[:0]
		s.onHeader(&hd)

		if s.IsNeedSubPacket() {
			s.subPacketBuf.Write(remain)
			return
		}
		if s.IsDataPacket() {
			s.consumeDataPacket(remain)
			return
		}
		if s.gotZFin() {
			if len(remain) > 0 {
				s.consume(remain)
			}
			return
		}
		s.headerBuf = append(s.headerBuf, remain...)
	}
}

// consumeDataPacket skips the encoded file payload until its subpacket trailer.
// Scanning payload bytes for frame prefixes can mistake ordinary file content for
// ZFILE/ZEOF headers and create duplicate audit records.
func (s *ZSession) consumeDataPacket(p []byte) {
	for i := 0; i < len(p); i++ {
		value := p[i]

		if s.dataCRCRemaining > 0 {
			if s.dataGotZDLE {
				if isFlowControl(value) {
					continue
				}
				s.dataGotZDLE = false
				s.dataCRCRemaining--
			} else {
				if isFlowControl(value) {
					continue
				}
				if value == ZDLE {
					s.dataGotZDLE = true
					continue
				}
				s.dataCRCRemaining--
			}

			if s.dataCRCRemaining == 0 && s.dataFrameEnds {
				s.dataGotZDLE = false
				s.dataFrameEnds = false
				s.currentHd = nil
				if i+1 < len(p) {
					s.consumeHeader(p[i+1:])
				}
				return
			}
			continue
		}

		if s.dataGotZDLE {
			if isFlowControl(value) {
				continue
			}
			s.dataGotZDLE = false
			switch value {
			case ZCRCE, ZCRCW:
				s.dataFrameEnds = true
				s.dataCRCRemaining = s.frameCRCSize
			case ZCRCG, ZCRCQ:
				s.dataFrameEnds = false
				s.dataCRCRemaining = s.frameCRCSize
			}
			continue
		}

		if value == ZDLE {
			s.dataGotZDLE = true
		}
	}
}

func isFlowControl(value byte) bool {
	switch value {
	case 0x11, 0x13, 0x91, 0x93:
		return true
	default:
		return false
	}
}

func (s *ZSession) consumeSubPacket() {
	buf := s.subPacketBuf.Bytes()
	if len(buf) == 0 {
		return
	}
	var offset = -1
	for i := range buf {
		value := buf[i]
		if s.subPacketGotZDLE {
			switch value {
			case ZCRCE, ZCRCG, ZCRCQ, ZCRCW:
				offset = i
				s.subPacketGotZDLE = false
			default:
				if !s.appendSubPacketByte(value ^ 0x40) {
					return
				}
				s.subPacketGotZDLE = false
			}
			if offset != -1 {
				break
			}
			continue
		}

		switch value {
		case ZDLE:
			s.subPacketGotZDLE = true
		case 0x91, 0x13, 0x11:
			continue
		default:
			if !s.appendSubPacketByte(value) {
				return
			}
		}
	}

	if offset == -1 {
		s.subPacketBuf.Reset()
		return
	}

	remain := append([]byte(nil), buf[offset+1:]...)
	s.subPacketBuf.Reset()
	s.onSubPacket(s.parsedSubPacket)
	s.parsedSubPacket = nil
	if len(remain) > 0 {
		s.consume(remain)
	}
}

func (s *ZSession) appendSubPacketByte(value byte) bool {
	if len(s.parsedSubPacket) >= maxZFileSubPacketSize {
		logger.Errorf("Zmodem %s ZFILE subpacket exceeds %d bytes", s.Type, maxZFileSubPacketSize)
		s.transferStatus = TransferStatusAbort
		s.parsedSubPacket = nil
		s.subPacketBuf.Reset()
		s.subPacketGotZDLE = false
		return false
	}
	s.parsedSubPacket = append(s.parsedSubPacket, value)
	return true
}

func findFrameHeader(p []byte) (int, byte) {
	index := -1
	headerType := byte(0)
	for _, item := range []struct {
		prefix     []byte
		headerType byte
	}{
		{HexHeaderPrefix, ZHEX},
		{Binary16HeaderPrefix, ZBIN},
		{Binary32HeaderPrefix, ZBIN32},
	} {
		current := bytes.Index(p, item.prefix)
		if current != -1 && (index == -1 || current < index) {
			index = current
			headerType = item.headerType
		}
	}
	return index, headerType
}

func (s *ZSession) retainHeaderPrefix() {
	maxPrefixLen := len(HexHeaderPrefix)
	if len(Binary16HeaderPrefix) > maxPrefixLen {
		maxPrefixLen = len(Binary16HeaderPrefix)
	}
	if len(Binary32HeaderPrefix) > maxPrefixLen {
		maxPrefixLen = len(Binary32HeaderPrefix)
	}
	retain := maxPrefixLen - 1
	if len(s.headerBuf) > retain {
		s.headerBuf = append(s.headerBuf[:0], s.headerBuf[len(s.headerBuf)-retain:]...)
	}
}

func (s *ZSession) onSubPacket(p []byte) {
	switch s.currentHd.Type {
	case ZFILE:
		var info ZFileInfo
		filenameIndex := bytes.IndexByte(p, 0x00)
		if filenameIndex == -1 {
			logger.Errorf("Zmodem %s invalid ZFILE metadata: filename terminator missing", s.Type)
			break
		}
		filename := p[:filenameIndex]
		if !validZFileName(filename) {
			logger.Errorf("Zmodem %s invalid ZFILE filename", s.Type)
			break
		}
		remain := p[filenameIndex+1:]
		zFileOptions := bytes.Fields(remain)
		if len(zFileOptions) == 0 {
			logger.Errorf("Zmodem %s invalid ZFILE metadata: file size missing", s.Type)
			break
		}
		size, err := strconv.Atoi(string(zFileOptions[0]))
		if err != nil || size < 0 {
			logger.Errorf("Zmodem %s invalid ZFILE size", s.Type)
			break
		}

		info.filename = string(filename)
		info.size = size
		info.parserTime = time.Now()
		info.transferType = s.Type
		s.zFileInfo = &info
		if s.ZFileHeaderCallback != nil {
			s.ZFileHeaderCallback(&info)
		}
	}
	s.currentHd = nil
}

func validZFileName(filename []byte) bool {
	if len(filename) == 0 || len(filename) > maxZFileNameSize {
		return false
	}
	for _, value := range filename {
		if value < 0x20 || value == 0x7f {
			return false
		}
	}
	return true
}

func (s *ZSession) onHeader(hd *ZmodemHeader) {
	switch hd.Type {
	case ZFILE:
		s.transferStatus = TransferStatusStart
	case ZDATA:
		s.dataGotZDLE = false
		s.dataCRCRemaining = 0
		s.dataFrameEnds = false
	case ZEOF:
		s.transferStatus = TransferStatusFinished
		s.zFileInfo = nil
	case ZFIN:
		//s.haveEnd = true
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

func (s *ZSession) IsDataPacket() bool {
	return s.currentHd != nil && s.currentHd.Type == ZDATA
}

func (s *ZSession) gotZFin() bool {
	return s.currentHd != nil && s.currentHd.Type == ZFIN
}

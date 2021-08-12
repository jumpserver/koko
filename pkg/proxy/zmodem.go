package proxy

import (
	"bytes"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jumpserver/koko/pkg/logger"
)

//FRAME TYPES
const (
	ZRQINIT    = 0x00 /* request receive init (s->r) */
	ZRINIT     = 0x01 /* receive init (r->s) */
	ZSINIT     = 0x02 /* send init sequence (optional) (s->r) */
	ZACK       = 0x03 /* ack to ZRQINIT ZRINIT or ZSINIT (s<->r) */
	ZFILE      = 0x04 /* file name (s->r) */
	ZSKIP      = 0x05 /* skip this file (r->s) */
	ZNAK       = 0x06 /* last packet was corrupted (?) */
	ZABORT     = 0x07 /* abort batch transfers (?) */
	ZFIN       = 0x08 /* finish session (s<->r) */
	ZRPOS      = 0x09 /* resume data transmission here (r->s) */
	ZDATA      = 0x0a /* data packet(s) follow (s->r) */
	ZEOF       = 0x0b /* end of file reached (s->r) */
	ZFERR      = 0x0c /* fatal read or write error detected (?) */
	ZCRC       = 0x0d /* request for file CRC and response (?) */
	ZCHALLENGE = 0x0e /* security challenge (r->s) */
	ZCOMPL     = 0x0f /* request is complete (?) */
	ZCAN       = 0x10 /* pseudo frame; other end cancelled session with 5* CAN */
	ZFREECNT   = 0x11 /* request free bytes on file system (s->r) */
	ZCOMMAND   = 0x12 /* issue command (s->r) */
	ZSTDERR    = 0x13 /* output data to stderr (??) */
)

type FrameType byte

func (t FrameType) String() string {
	switch t {
	case ZRQINIT:
		return "ZRQINIT"
	case ZRINIT:
		return "ZRINIT"
	case ZSINIT:
		return "ZSINIT"
	case ZACK:
		return "ZACK"
	case ZFILE:
		return "ZFILE"
	case ZSKIP:
		return "ZSKIP"
	case ZNAK:
		return "ZNAK"
	case ZABORT:
		return "ZABORT"
	case ZFIN:
		return "ZFIN"
	case ZRPOS:
		return "ZRPOS"
	case ZDATA:
		return "ZDATA"
	case ZEOF:
		return "ZEOF"
	case ZFERR:
		return "ZFERR"
	case ZCRC:
		return "ZCRC"
	case ZCHALLENGE:
		return "ZCHALLENGE"
	case ZCOMPL:
		return "ZCOMPL"
	case ZCAN:
		return "ZCAN"
	case ZFREECNT:
		return ""
	case ZCOMMAND:
		return "ZCOMMAND"
	case ZSTDERR:
		return "ZSTDERR"
	}
	return "UNKNOWN"
}

const (
	ZDLE = 0x18 /* ctrl-x zmodem escape */

	// ZPAD *
	ZPAD = 0x2a /* pad character; begins frames */

	// ZBIN A
	ZBIN = 0x41 /* binary frame indicator (CRC16) */

	// ZHEX B
	ZHEX = 0x42 /* hex frame indicator */

	// ZBIN32 C
	ZBIN32 = 0x43 /* binary frame indicator (CRC32) */

	CAN = 0x18
)

//ZDLE SEQUENCES
const (
	ZCRCE = 0x68 /* CRC next, frame ends, header packet follows */
	ZCRCG = 0x69 /* CRC next, frame continues nonstop */
	ZCRCQ = 0x6a /* CRC next, frame continuous, ZACK expected */
	ZCRCW = 0x6b /* CRC next, ZACK expected, end of frame */

	ZRUB0 = 0x6c /* translate to rubout 0x7f */
	ZRUB1 = 0x6d /* translate to rubout 0xff */
)

var (
	HexHeaderPrefix = []byte{ZPAD, ZPAD, ZDLE, ZHEX}

	Binary16HeaderPrefix = []byte{ZPAD, ZDLE, ZBIN}

	Binary32HeaderPrefix = []byte{ZPAD, ZDLE, ZBIN32}

	AbortSession = []byte{CAN, CAN, CAN, CAN, CAN}

	CancelSequence = []byte{CAN, CAN, CAN, CAN, CAN, CAN, CAN, CAN, CAN, CAN,
		0x08, 0x08, 0x08, 0x08, 0x08, 0x08, 0x08, 0x08, 0x08, 0x08}
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

const (
	ZParserStatusNone    = ""
	ZParserStatusSend    = "Send"
	ZParserStatusReceive = "Receive"
)

type ZmodemParser struct {
	sync.Mutex
	currentSession *ZSession

	status atomic.Value

	fileEventCallback func(zinfo *ZFileInfo, status bool)

	currentZFileInfo *ZFileInfo

	currentHeader *ZmodemHeader

	abortMark       bool // 不记录中断的文件
	hasDataTransfer bool
}

// rz sz 解析的入口

func (z *ZmodemParser) Parse(p []byte) {
	z.Lock()
	defer z.Unlock()
	if z.IsStartSession() {
		zSession := z.currentSession
		zSession.consume(p)
		if zSession.IsEnd() {
			z.currentSession = nil
			if z.fileEventCallback != nil && z.currentZFileInfo != nil {
				info := z.currentZFileInfo
				transferStatus := false
				if zSession.transferStatus != TransferStatusAbort {
					transferStatus = true
				}
				if !z.abortMark {
					z.fileEventCallback(info, transferStatus)
				}
				z.currentZFileInfo = nil
				z.hasDataTransfer = false
			}
			logger.Infof("Zmodem session %s end", z.Status())
			z.setStatus(ZParserStatusNone)
		}
		return
	}

	index := bytes.IndexByte(p, ZDLE)
	if index == -1 {
		return
	}
	remain := p[index:]
	hd := z.ParseHexHeader(remain)
	if hd == nil {
		return
	}
	switch hd.Type {
	case ZRQINIT:
		z.currentSession = &ZSession{
			Type: TypeDownload,
			endCallback: func() {
				z.setStatus(ZParserStatusNone)
			},
			ZFileHeaderCallback: z.zFileFrameCallback,
			zOnHeader:           z.OnHeader,
		}
		z.status.Store(ZParserStatusSend)
	case ZRINIT:
		z.currentSession = &ZSession{
			Type: TypeUpload,
			endCallback: func() {
				z.setStatus(ZParserStatusNone)
			},
			ZFileHeaderCallback: z.zFileFrameCallback,
			zOnHeader:           z.OnHeader,
		}
		z.setStatus(ZParserStatusReceive)
	default:
		z.currentSession = nil
		z.abortMark = false
		z.setStatus(ZParserStatusNone)
	}
}

func (z *ZmodemParser) IsStartSession() bool {
	return z.Status() != ZParserStatusNone
}

func (z *ZmodemParser) Status() string {
	return z.status.Load().(string)
}
func (z *ZmodemParser) setStatus(status string) {
	z.status.Store(status)
}

func (z *ZmodemParser) SessionType() string {
	if z.currentSession != nil {
		return z.currentSession.Type
	}
	return ""
}

func (z *ZmodemParser) setAbortMark() {
	// 不记录中断的文件
	z.abortMark = true
}

func (z *ZmodemParser) OnHeader(hd *ZmodemHeader) {
	z.currentHeader = hd
	switch hd.Type {
	case ZEOF, ZFILE:
		if z.fileEventCallback != nil && z.currentZFileInfo != nil {
			z.fileEventCallback(z.currentZFileInfo, true)
		}
		z.currentZFileInfo = nil
		z.hasDataTransfer = false
	case ZDATA:
		z.hasDataTransfer = true
	case ZFIN:
		if !z.abortMark {
			if z.fileEventCallback != nil && z.currentZFileInfo != nil {
				status := true
				if !z.hasDataTransfer && z.currentZFileInfo.size > 0 {
					/*
					 如果没有文件传输，且文件大小大于0， 则代表下载失败
					*/
					status = false
				}
				z.fileEventCallback(z.currentZFileInfo, status)
			}
		}
		z.currentZFileInfo = nil
		z.hasDataTransfer = false
	}
}

func (z *ZmodemParser) zFileFrameCallback(info *ZFileInfo) {
	z.currentZFileInfo = info
	logger.Infof("Zmodem parser got filename: %s siz: %d", info.filename, info.size)
}

func (z *ZmodemParser) IsZFilePacket() bool {
	return z.currentHeader != nil && z.currentHeader.Type == ZFILE
}

func (z *ZmodemParser) GetCurrentZFileInfo() *ZFileInfo {
	return z.currentZFileInfo
}

func (z *ZmodemParser) ParseHexHeader(p []byte) *ZmodemHeader {
	endPos := bytes.IndexByte(p, 0x8a)
	if endPos == -1 {
		endPos = bytes.IndexByte(p, 0x0a)
	}
	if endPos == -1 {
		return nil
	}
	hexBytes := p[:endPos+1]
	hexBytes = bytes.TrimSpace(hexBytes)
	if len(hexBytes) != 18 {
		return nil
	}
	hexBytes = hexBytes[2:]
	octets := ConvertHexToOctets(hexBytes)
	return ParseNonZDLEBinary16(octets)
}

func (z *ZmodemParser) Cleanup() {
	if z.IsStartSession() {
		if z.fileEventCallback != nil && z.currentZFileInfo != nil {
			z.fileEventCallback(z.currentZFileInfo, false)
		}
	}
}

func ParseNonZDLEBinary16(p []byte) *ZmodemHeader {
	if len(p) < bin16HeaderLen {
		return nil
	}
	// todo 校验 crc-1 crc-2 ?
	return &ZmodemHeader{
		Type: p[0],
		ZF0:  p[1],
		ZF1:  p[2],
		ZF2:  p[3],
		ZF3:  p[4],
	}
}

func ConvertHexToOctets(p []byte) []byte {
	octets := make([]byte, len(p)/2)
	for i := 0; i < len(octets); i++ {
		value := (HexOctetValue[p[2*i]] << 4) + HexOctetValue[p[1+2*i]]
		octets[i] = uint8(value)
	}
	return octets

}

var HexOctetValue = InitHexOctetValue()

func InitHexOctetValue() map[byte]int {
	ret := map[byte]int{}
	hexValue := []byte{
		'0', '1', '2', '3',
		'4', '5', '6', '7',
		'8', '9', 'a', 'b',
		'c', 'd', 'e', 'f',
	}
	for i, value := range hexValue {
		ret[value] = i
	}
	return ret
}

var skipSequence = []byte{
	0x2a, 0x2a, 0x18, 0x42,
	0x30, 0x35, 0x30, 0x30,
	0x30, 0x30, 0x30, 0x30,
	0x36, 0x33, 0x37, 0x66,
	0x39, 0x32, 0x0d, 0x8a, 0x11,
}

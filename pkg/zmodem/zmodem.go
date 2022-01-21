package zmodem

import (
	"bytes"
	"sync"
	"sync/atomic"

	"github.com/jumpserver/koko/pkg/logger"
)

const (
	ZParserStatusNone    = ""
	ZParserStatusSend    = "Send"
	ZParserStatusReceive = "Receive"
)

func New() *ZmodemParser {
	var p ZmodemParser
	p.setStatus(ZParserStatusNone)
	return &p
}

type ZmodemParser struct {
	sync.Mutex
	currentSession *ZSession

	status atomic.Value

	FileEventCallback func(zinfo *ZFileInfo, status bool)

	currentZFileInfo *ZFileInfo

	currentHeader *ZmodemHeader

	abortMark       bool // 不记录中断的文件
	hasDataTransfer bool

	FireStatusEvent func(event StatusEvent)
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
			if z.FileEventCallback != nil && z.currentZFileInfo != nil {
				info := z.currentZFileInfo
				transferStatus := false
				if zSession.transferStatus != TransferStatusAbort {
					transferStatus = true
				}
				if !z.abortMark {
					z.FileEventCallback(info, transferStatus)
				}
				z.currentZFileInfo = nil
				z.hasDataTransfer = false
			}
			logger.Infof("Zmodem session %s end", z.Status())
			z.setStatus(ZParserStatusNone)
			if z.FireStatusEvent != nil {
				z.FireStatusEvent(EndEvent)
			}
		}
		return
	}

	index := bytes.IndexByte(p, ZDLE)
	if index == -1 {
		return
	}
	remain := p[index:]
	nr, hd := ParseHexHeader(remain)
	if hd == nil {
		return
	}
	remain = remain[nr+1:]
	switch hd.Type {
	case ZRQINIT:
		z.currentSession = &ZSession{
			Type: TypeDownload,
			endCallback: func() {
				z.setStatus(ZParserStatusNone)
				if z.FireStatusEvent != nil {
					z.FireStatusEvent(EndEvent)
				}
			},
			ZFileHeaderCallback: z.zFileFrameCallback,
			zOnHeader:           z.OnHeader,
		}
		z.setStatus(ZParserStatusSend)
		if z.FireStatusEvent != nil {
			z.FireStatusEvent(StartEvent)
		}
		z.currentSession.consume(remain)
	case ZRINIT:
		z.currentSession = &ZSession{
			Type: TypeUpload,
			endCallback: func() {
				z.setStatus(ZParserStatusNone)
				if z.FireStatusEvent != nil {
					z.FireStatusEvent(EndEvent)
				}
			},
			ZFileHeaderCallback: z.zFileFrameCallback,
			zOnHeader:           z.OnHeader,
		}
		z.setStatus(ZParserStatusReceive)
		if z.FireStatusEvent != nil {
			z.FireStatusEvent(StartEvent)
		}
		z.currentSession.consume(remain)
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

func (z *ZmodemParser) SetAbortMark() {
	// 不记录中断的文件
	z.abortMark = true
}

func (z *ZmodemParser) OnHeader(hd *ZmodemHeader) {
	z.currentHeader = hd
	switch hd.Type {
	case ZEOF, ZFILE:
		if z.FileEventCallback != nil && z.currentZFileInfo != nil {
			z.FileEventCallback(z.currentZFileInfo, true)
		}
		z.currentZFileInfo = nil
		z.hasDataTransfer = false
	case ZDATA:
		z.hasDataTransfer = true
	case ZFIN:
		if !z.abortMark {
			if z.FileEventCallback != nil && z.currentZFileInfo != nil {
				status := true
				if !z.hasDataTransfer && z.currentZFileInfo.size > 0 {
					/*
					 如果没有文件传输，且文件大小大于0， 则代表下载失败
					*/
					status = false
				}
				z.FileEventCallback(z.currentZFileInfo, status)
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

func (z *ZmodemParser) Cleanup() {
	if z.IsStartSession() {
		if z.FileEventCallback != nil && z.currentZFileInfo != nil {
			z.FileEventCallback(z.currentZFileInfo, false)
		}
	}
}

func ParseHexHeader(p []byte) (int, *ZmodemHeader) {
	endPos := bytes.IndexByte(p, 0x8a)
	if endPos == -1 {
		endPos = bytes.IndexByte(p, 0x0a)
	}
	if endPos == -1 {
		return 0, nil
	}
	hexBytes := p[:endPos+1]
	hexBytes = bytes.TrimSpace(hexBytes)
	if len(hexBytes) != 18 {
		return 0, nil
	}
	hexBytes = hexBytes[2:]
	octets := ConvertHexToOctets(hexBytes)
	return endPos, ParseNonZDLEBinary16(octets)
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

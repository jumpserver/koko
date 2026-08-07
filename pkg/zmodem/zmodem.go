package zmodem

import (
	"bytes"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jumpserver/koko/pkg/logger"
)

const (
	ZParserStatusNone    = ""
	ZParserStatusSend    = "Send"
	ZParserStatusReceive = "Receive"

	initialHeaderBufferLimit = 256
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

	lastActiveUnixNano atomic.Int64
	sessionStartedNano atomic.Int64
	sendHandshakeDone  atomic.Bool
	initialHeaderBuf   []byte

	FileEventCallback func(zinfo *ZFileInfo, status bool)

	currentZFileInfo *ZFileInfo

	currentHeader *ZmodemHeader

	abortMark       bool // 不记录中断的文件
	hasDataTransfer bool

	FireStatusEvent func(event StatusEvent)

	AbnormalFinish bool
	lastAbort      bool
}

// rz sz 解析的入口

func (z *ZmodemParser) Parse(p []byte) {
	z.Lock()
	defer z.Unlock()
	if z.IsStartSession() {
		z.touch()
		zSession := z.currentSession
		zSession.consume(p)
		if zSession.IsEnd() {
			z.finishSession(zSession, true)
		}
		return
	}

	z.consumeInitialHeader(p)
}

func (z *ZmodemParser) consumeInitialHeader(p []byte) {
	z.initialHeaderBuf = append(z.initialHeaderBuf, p...)
	for len(z.initialHeaderBuf) > 0 {
		index := bytes.Index(z.initialHeaderBuf, HexHeaderPrefix)
		if index == -1 {
			retain := len(HexHeaderPrefix) - 1
			if len(z.initialHeaderBuf) > retain {
				z.initialHeaderBuf = append(
					z.initialHeaderBuf[:0],
					z.initialHeaderBuf[len(z.initialHeaderBuf)-retain:]...,
				)
			}
			return
		}

		candidate := z.initialHeaderBuf[index:]
		hd, offset, ok := DecodeHexFrameHeader(candidate)
		if !ok {
			hasTerminator := bytes.IndexByte(candidate, 0x8a) != -1 ||
				bytes.IndexByte(candidate, 0x0a) != -1
			if !hasTerminator && len(candidate) <= initialHeaderBufferLimit {
				z.initialHeaderBuf = append(z.initialHeaderBuf[:0], candidate...)
				return
			}
			// 丢弃损坏帧的第一个字节，继续寻找下一帧，避免解析器永久卡住。
			z.initialHeaderBuf = append(z.initialHeaderBuf[:0], candidate[1:]...)
			continue
		}

		remain := append([]byte(nil), candidate[offset+1:]...)
		z.initialHeaderBuf = z.initialHeaderBuf[:0]
		switch hd.Type {
		case ZRQINIT:
			z.startSession(TypeDownload, ZParserStatusSend, remain)
			return
		case ZRINIT:
			z.startSession(TypeUpload, ZParserStatusReceive, remain)
			return
		default:
			z.initialHeaderBuf = append(z.initialHeaderBuf, remain...)
		}
	}
}

func (z *ZmodemParser) startSession(sessionType, status string, remain []byte) {
	z.currentSession = &ZSession{
		Type:                sessionType,
		ZFileHeaderCallback: z.zFileFrameCallback,
		zOnHeader:           z.OnHeader,
	}
	z.currentHeader = nil
	z.currentZFileInfo = nil
	z.abortMark = false
	z.hasDataTransfer = false
	z.AbnormalFinish = false
	z.lastAbort = false
	z.sessionStartedNano.Store(time.Now().UnixNano())
	z.sendHandshakeDone.Store(false)
	z.setStatus(status)
	z.touch()
	if z.FireStatusEvent != nil {
		z.FireStatusEvent(StartEvent)
	}
	if len(remain) > 0 {
		z.currentSession.consume(remain)
		if z.currentSession.IsEnd() {
			z.finishSession(z.currentSession, true)
		}
	}
}

func (z *ZmodemParser) finishSession(session *ZSession, fireStatusEvent bool) {
	status := z.Status()
	z.AbnormalFinish = session != nil && session.AbnormalFinish
	z.lastAbort = session != nil && session.transferStatus == TransferStatusAbort
	fileInfo := z.currentZFileInfo
	shouldFireFileEvent := z.FileEventCallback != nil && fileInfo != nil && !z.abortMark
	transferStatus := session != nil && session.transferStatus != TransferStatusAbort
	z.currentSession = nil
	z.currentZFileInfo = nil
	z.currentHeader = nil
	z.initialHeaderBuf = z.initialHeaderBuf[:0]
	z.hasDataTransfer = false
	z.abortMark = false
	z.lastActiveUnixNano.Store(0)
	z.sessionStartedNano.Store(0)
	z.sendHandshakeDone.Store(false)
	z.setStatus(ZParserStatusNone)
	logger.Infof("Zmodem session %s end", status)
	if fireStatusEvent && z.FireStatusEvent != nil {
		event := EndEvent
		if session != nil && session.transferStatus == TransferStatusAbort {
			event = AbortEvent
		}
		z.FireStatusEvent(event)
	}
	if shouldFireFileEvent {
		z.FileEventCallback(fileInfo, transferStatus)
	}
}

// ConsumeLastAbort reports an aborted session once, so its terminal output can be cleaned up.
func (z *ZmodemParser) ConsumeLastAbort() bool {
	z.Lock()
	defer z.Unlock()
	aborted := z.lastAbort
	z.lastAbort = false
	return aborted
}

func (z *ZmodemParser) touch() {
	z.lastActiveUnixNano.Store(time.Now().UnixNano())
}

// MarkActive refreshes the idle deadline for protocol data flowing in either direction.
func (z *ZmodemParser) MarkActive() {
	if z.IsStartSession() {
		z.touch()
	}
}

// IsExpired reports whether an active transfer has stopped producing protocol data.
func (z *ZmodemParser) IsExpired(now time.Time, idleTimeout time.Duration) bool {
	if !z.IsStartSession() || idleTimeout <= 0 {
		return false
	}
	lastActive := z.lastActiveUnixNano.Load()
	return lastActive > 0 && now.Sub(time.Unix(0, lastActive)) >= idleTimeout
}

// IsSendHandshakeExpired prevents repeated ZRQINIT frames from keeping a failed sz session alive forever.
func (z *ZmodemParser) IsSendHandshakeExpired(now time.Time, timeout time.Duration) bool {
	if z.Status() != ZParserStatusSend || timeout <= 0 || z.sendHandshakeDone.Load() {
		return false
	}
	started := z.sessionStartedNano.Load()
	return started > 0 && now.Sub(time.Unix(0, started)) >= timeout
}

// Abort resets an active session and emits one abort event.
func (z *ZmodemParser) Abort() bool {
	z.Lock()
	defer z.Unlock()
	if !z.IsStartSession() {
		return false
	}
	if z.currentSession != nil {
		z.currentSession.transferStatus = TransferStatusAbort
		z.currentSession.AbnormalFinish = true
	}
	z.finishSession(z.currentSession, true)
	return true
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
	if z.Status() == ZParserStatusSend {
		z.sendHandshakeDone.Store(true)
	}
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
	z.Lock()
	defer z.Unlock()
	if !z.IsStartSession() {
		return
	}
	if z.currentSession != nil {
		z.currentSession.transferStatus = TransferStatusAbort
	}
	z.finishSession(z.currentSession, false)
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

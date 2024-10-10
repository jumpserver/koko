package proxy

import (
	"strings"
	"sync"

	"github.com/LeeEirc/terminalparser"

	"github.com/jumpserver/koko/pkg/logger"
)

type RingBuffer struct {
	data   []byte
	size   int
	start  int
	end    int
	length int
}

func (rb *RingBuffer) Write(p []byte) {
	n := len(p)
	for i := 0; i < n; i++ {
		rb.data[rb.end] = p[i]
		rb.end = (rb.end + 1) % rb.size
		if rb.length == rb.size {
			// 覆盖旧数据，start 也要前移
			rb.start = (rb.start + 1) % rb.size
		} else {
			rb.length++
		}
	}
}

func (rb *RingBuffer) Bytes() []byte {
	p := make([]byte, rb.length)
	for i := 0; i < rb.length; i++ {
		p[i] = rb.data[(rb.start+i)%rb.size]
	}
	return p
}

func (rb *RingBuffer) Reset() {
	rb.start = 0
	rb.end = 0
	rb.length = 0
	for i := 0; i < rb.size; i++ {
		rb.data[i] = 0
	}
}

func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{
		data: make([]byte, size),
		size: size,
	}
}

const maxBufSize = 1024 * 100

func NewCmdParser(sid, name string) *CmdParser {
	parser := CmdParser{id: sid, name: name, buf: NewRingBuffer(maxBufSize)}
	return &parser
}

type CmdParser struct {
	id   string
	name string

	buf  *RingBuffer
	lock sync.Mutex

	ps1 string
}

func (cp *CmdParser) WriteData(p []byte) (int, error) {
	cp.lock.Lock()
	defer cp.lock.Unlock()
	cp.buf.Write(p)
	return len(p), nil
}

func (cp *CmdParser) Close() error {
	logger.Infof("session ID: %s, ParseEngine name: %s Close", cp.id, cp.name)
	return nil
}

func (cp *CmdParser) removePs1(s string) string {
	// 通过去除Ps1 获取完整的命令
	return strings.TrimPrefix(s, cp.ps1)
}

// Parse 解析命令或输出
func (cp *CmdParser) Parse() []string {
	cp.lock.Lock()
	defer cp.lock.Unlock()
	lines := make([]string, 0, 100)
	for _, line := range cp.parse(cp.buf.Bytes()) {
		line = cp.removePs1(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	cp.buf.Reset()
	return lines
}

func (cp *CmdParser) GetPs1() string {
	cp.lock.Lock()
	defer cp.lock.Unlock()
	lines := cp.parse(cp.buf.Bytes())
	if len(lines) == 0 {
		return ""
	}
	cp.ps1 = lines[len(lines)-1]
	// output的最后行大概率可能是 ps1
	return cp.ps1
}

func (cp *CmdParser) SetPs1(s string) {
	cp.lock.Lock()
	defer cp.lock.Unlock()
	cp.ps1 = s
}

func (cp *CmdParser) parse(p []byte) []string {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("[%s] %s panic: %s\n", cp.id, cp.name, r)
		}
	}()
	s := terminalparser.Screen{
		Rows:   make([]*terminalparser.Row, 0, 1024),
		Cursor: &terminalparser.Cursor{},
	}
	return s.Parse(p)
}

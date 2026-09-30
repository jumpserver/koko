package tunnel

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"sync"

	"github.com/jumpserver/koko/pkg/lion/guacd"
	"github.com/jumpserver/koko/pkg/lion/proxy"
	"github.com/jumpserver/koko/pkg/logger"

	"github.com/jumpserver-dev/sdk-go/model"
)

type OutputStreamInterceptingFilter struct {
	sync.Mutex
	tunnel           *Connection
	streams          map[string]*OutStreamResource
	acknowledgeBlobs bool
	closed           bool
}

func (filter *OutputStreamInterceptingFilter) Filter(instruction *guacd.Instruction) *guacd.Instruction {
	switch instruction.Opcode {
	case guacd.InstructionStreamingBlob:
		return filter.handleBlob(instruction)
	case guacd.InstructionStreamingEnd:
		filter.handleEnd(instruction)
	case guacd.InstructionClientSync:
		filter.handleSync()
	}
	return instruction
}

func (filter *OutputStreamInterceptingFilter) handleBlob(instruction *guacd.Instruction) *guacd.Instruction {
	if len(instruction.Args) < 2 {
		return instruction
	}
	index := instruction.Args[0]
	filter.Lock()
	stream := filter.streams[index]
	filter.Unlock()
	if stream == nil {
		return instruction
	}

	blob, err := base64.StdEncoding.DecodeString(instruction.Args[1])
	if err != nil {
		filter.finishOutStream(index, stream, fmt.Errorf("decode guacamole stream: %w", err))
		return nil
	}
	if _, err = stream.writer.Write(blob); err != nil {
		filter.finishOutStream(index, stream, err)
		if ackErr := filter.sendAck(index, "FAIL", guacd.StatusServerError); ackErr != nil {
			logger.Errorf("OutputStream filter sendAck err: %+v", ackErr)
		}
		return nil
	}
	if err = stream.recorder.RecordWrite(stream.ftpLog, blob); err != nil {
		logger.Errorf("OutputStream filter stream %s record write err: %+v", stream.streamIndex, err)
	}
	if !filter.acknowledgeBlobs {
		filter.acknowledgeBlobs = true
		response := guacd.NewInstruction(guacd.InstructionStreamingBlob, index, "")
		return &response
	}
	if err = filter.sendAck(index, "OK", guacd.StatusSuccess); err != nil {
		filter.finishOutStream(index, stream, err)
		logger.Errorf("OutputStream filter sendAck err: %+v", err)
	}
	return nil
}

func (filter *OutputStreamInterceptingFilter) handleSync() {
	filter.acknowledgeBlobs = false
}

func (filter *OutputStreamInterceptingFilter) handleEnd(instruction *guacd.Instruction) {
	if len(instruction.Args) < 1 {
		return
	}
	index := instruction.Args[0]
	filter.Lock()
	stream := filter.streams[index]
	filter.Unlock()
	if stream != nil {
		filter.finishOutStream(index, stream, nil)
	}
}

func (filter *OutputStreamInterceptingFilter) sendAck(index, msg string, status guacd.GuacamoleStatus) error {
	return filter.tunnel.WriteTunnelMessage(guacd.NewInstruction(
		guacd.InstructionStreamingAck, index, msg, strconv.Itoa(status.GuaCode)))
}

func (filter *OutputStreamInterceptingFilter) finishOutStream(index string, stream *OutStreamResource, err error) {
	filter.Lock()
	if filter.streams[index] == stream {
		delete(filter.streams, index)
	}
	filter.Unlock()
	stream.finish(err)
}

func (filter *OutputStreamInterceptingFilter) addOutStream(stream *OutStreamResource) {
	filter.Lock()
	if filter.closed {
		filter.Unlock()
		stream.finish(fmt.Errorf("guacamole download tunnel closed"))
		return
	}
	previous := filter.streams[stream.streamIndex]
	filter.streams[stream.streamIndex] = stream
	filter.Unlock()
	if previous != nil {
		previous.finish(fmt.Errorf("guacamole download stream replaced"))
	}
	if err := filter.sendAck(stream.streamIndex, "OK", guacd.StatusSuccess); err != nil {
		filter.finishOutStream(stream.streamIndex, stream, err)
	}
}

func (filter *OutputStreamInterceptingFilter) closeAll(err error) {
	filter.Lock()
	filter.closed = true
	streams := filter.streams
	filter.streams = make(map[string]*OutStreamResource)
	filter.Unlock()
	for _, stream := range streams {
		stream.finish(err)
	}
}

// OutStreamResource is a download stream received from guacd.
type OutStreamResource struct {
	streamIndex string
	mediaType   string // application/octet-stream
	writer      http.ResponseWriter
	done        chan struct{}
	ctx         context.Context

	once sync.Once
	err  error

	ftpLog   *model.FTPLog
	recorder *proxy.FTPFileRecorder
}

func (r *OutStreamResource) finish(err error) {
	r.once.Do(func() {
		r.err = err
		close(r.done)
	})
}

func (r *OutStreamResource) Wait() error {
	select {
	case <-r.done:
		return r.err
	case <-r.ctx.Done():
		return fmt.Errorf("closed request %s: %w", r.streamIndex, r.ctx.Err())
	}
}

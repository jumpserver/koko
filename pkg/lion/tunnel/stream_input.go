package tunnel

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"sync"

	"github.com/jumpserver/koko/pkg/lion/guacd"
)

type InputStreamInterceptingFilter struct {
	tunnel  *Connection
	streams map[string]*InputStreamResource
	closed  bool
	sync.Mutex
}

func (filter *InputStreamInterceptingFilter) Filter(instruction *guacd.Instruction) *guacd.Instruction {
	if instruction.Opcode == guacd.InstructionStreamingAck {
		filter.handleAck(instruction)
	}
	return instruction
}

func (filter *InputStreamInterceptingFilter) handleAck(instruction *guacd.Instruction) {
	if len(instruction.Args) < 3 {
		return
	}
	index, status := instruction.Args[0], instruction.Args[2]

	filter.Lock()
	stream := filter.streams[index]
	if stream != nil && status != "0" {
		delete(filter.streams, index)
	}
	filter.Unlock()
	if stream == nil {
		return
	}
	if status != "0" {
		stream.finish(fmt.Errorf("guacamole upload acknowledgement failed: %s", status))
		return
	}

	buf := make([]byte, 6048)
	n, err := stream.reader.Read(buf)
	if n > 0 {
		err = filter.tunnel.WriteTunnelMessage(guacd.NewInstruction(
			guacd.InstructionStreamingBlob, index, base64.StdEncoding.EncodeToString(buf[:n])))
	}
	if err == nil {
		return
	}
	if err == io.EOF {
		filter.finishInputStream(index, stream, nil)
		return
	}
	filter.finishInputStream(index, stream, err)
}

func (filter *InputStreamInterceptingFilter) addInputStream(stream *InputStreamResource) {
	filter.Lock()
	if filter.closed {
		filter.Unlock()
		stream.finish(fmt.Errorf("guacamole upload tunnel closed"))
		return
	}
	previous := filter.streams[stream.streamIndex]
	filter.streams[stream.streamIndex] = stream
	filter.Unlock()
	if previous != nil {
		previous.finish(fmt.Errorf("guacamole upload stream replaced"))
	}
	filter.handleAck(&guacd.Instruction{Opcode: guacd.InstructionStreamingAck, Args: []string{stream.streamIndex, "", "0"}})
}

func (filter *InputStreamInterceptingFilter) finishInputStream(index string, stream *InputStreamResource, err error) {
	filter.Lock()
	if filter.streams[index] == stream {
		delete(filter.streams, index)
	}
	filter.Unlock()
	stream.finish(err)
}

func (filter *InputStreamInterceptingFilter) closeAll(err error) {
	filter.Lock()
	filter.closed = true
	streams := filter.streams
	filter.streams = make(map[string]*InputStreamResource)
	filter.Unlock()
	for _, stream := range streams {
		stream.finish(err)
	}
}

// InputStreamResource is an upload stream sent to guacd.
type InputStreamResource struct {
	streamIndex string
	reader      io.ReadCloser
	done        chan struct{}

	once sync.Once
	err  error
}

func (r *InputStreamResource) finish(err error) {
	r.once.Do(func() {
		r.err = err
		if err != nil {
			_ = r.reader.Close()
		}
		close(r.done)
	})
}

func (r *InputStreamResource) Wait(ctx context.Context) error {
	select {
	case <-r.done:
		return r.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

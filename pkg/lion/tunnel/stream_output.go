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
	streams          map[string]OutStreamResource
	acknowledgeBlobs bool
}

func (filter *OutputStreamInterceptingFilter) Filter(unfilteredInstruction *guacd.Instruction) *guacd.Instruction {

	switch unfilteredInstruction.Opcode {
	case guacd.InstructionStreamingBlob:
		// Intercept "blob" instructions for in-progress streams
		//if (instruction.getOpcode().equals("blob"))
		//return handleBlob(instruction);
		return filter.handleBlob(unfilteredInstruction)
	case guacd.InstructionStreamingEnd:
		//// Intercept "end" instructions for in-progress streams
		//if (instruction.getOpcode().equals("end")) {
		//	handleEnd(instruction);
		//	return instruction;
		//}
		filter.handleEnd(unfilteredInstruction)
		return unfilteredInstruction
	case guacd.InstructionClientSync:
		// Monitor "sync" instructions to ensure the client does not starve
		// from lack of graphical updates
		//if (instruction.getOpcode().equals("sync")) {
		//	handleSync(instruction);
		//	return instruction;
		//}
		filter.handleSync(unfilteredInstruction)
		return unfilteredInstruction
	}

	// Pass instruction through untouched
	//return instruction
	return unfilteredInstruction
}

func (filter *OutputStreamInterceptingFilter) handleBlob(unfilteredInstruction *guacd.Instruction) *guacd.Instruction {
	// Verify all required arguments are present
	args := unfilteredInstruction.Args
	if len(args) < 2 {
		return unfilteredInstruction
	}
	index := args[0]
	if stream, ok := filter.streams[index]; ok {
		// Decode blob
		data := args[1]
		blob, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			logger.Errorf("Base64 decode blob err: %+v", err)
			return nil
		}

		_, err = stream.writer.Write(blob)
		if err != nil {
			stream.err = err
			logger.Errorf("OutputStream filter stream %s write err: %+v", stream.streamIndex, err)
			if err = filter.sendAck(index, "FAIL", guacd.StatusServerError); err != nil {
				logger.Errorf("OutputStream filter sendAck err: %+v", err)
			}
			return nil
		}
		if err1 := stream.recorder.RecordWrite(stream.ftpLog, blob); err1 != nil {
			logger.Errorf("OutputStream filter stream %s record write err: %+v", stream.streamIndex, err1)
		}
		if !filter.acknowledgeBlobs {
			filter.acknowledgeBlobs = true
			ins := guacd.NewInstruction(guacd.InstructionStreamingBlob, index, "")
			return &ins
		}

		err = filter.sendAck(index, "Ok", guacd.StatusSuccess)
		if err != nil {
			logger.Errorf("OutputStream filter sendAck err: %+v", err)
		}
		return nil
	}
	return unfilteredInstruction
}

func (filter *OutputStreamInterceptingFilter) handleSync(unfilteredInstruction *guacd.Instruction) {
	filter.acknowledgeBlobs = false
}

func (filter *OutputStreamInterceptingFilter) handleEnd(unfilteredInstruction *guacd.Instruction) {
	// Verify all required arguments are present
	//List<String> args = instruction.getArgs();
	//if (args.size() < 1)
	//return;

	// Terminate stream
	//closeInterceptedStream(args.get(0));
	args := unfilteredInstruction.Args
	if len(args) < 1 {
		return
	}
	filter.closeInterceptedStream(args[0])
}

func (filter *OutputStreamInterceptingFilter) sendAck(index, msg string, status guacd.GuacamoleStatus) error {

	// Error "ack" instructions implicitly close the stream
	//if (status != GuacamoleStatus.SUCCESS)
	//	closeInterceptedStream(index);
	//
	//sendInstruction(new GuacamoleInstruction("ack", index, message,
	//	Integer.toString(status.getGuacamoleStatusCode())));
	if status.HttpCode != guacd.StatusSuccess.HttpCode {
		filter.closeInterceptedStream(index)
	}
	return filter.tunnel.WriteTunnelMessage(guacd.NewInstruction(
		guacd.InstructionStreamingAck, index, msg,
		strconv.Itoa(status.GuaCode)))

}

func (filter *OutputStreamInterceptingFilter) closeInterceptedStream(index string) {
	filter.Lock()
	defer filter.Unlock()
	if outStream, ok := filter.streams[index]; ok {
		close(outStream.done)
	}
	delete(filter.streams, index)
}

func (filter *OutputStreamInterceptingFilter) addOutStream(out OutStreamResource) {
	filter.Lock()
	defer filter.Unlock()
	err := filter.sendAck(out.streamIndex, "OK", guacd.StatusSuccess)
	if err != nil {
		logger.Errorf("OutputStream filter sendAck index %s err: %+v", out.streamIndex, err)
		out.err = err
		close(out.done)
		return
	}
	filter.streams[out.streamIndex] = out
}

// 下载文件的对象

type OutStreamResource struct {
	streamIndex string
	mediaType   string // application/octet-stream
	writer      http.ResponseWriter
	done        chan struct{}
	err         error
	ctx         context.Context

	ftpLog   *model.FTPLog
	recorder *proxy.FTPFileRecorder
}

func (r *OutStreamResource) Wait() error {
	select {
	case <-r.done:
	case <-r.ctx.Done():
		return fmt.Errorf("closed request %s", r.streamIndex)
	}
	return r.err
}

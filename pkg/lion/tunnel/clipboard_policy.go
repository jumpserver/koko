package tunnel

import (
	"encoding/base64"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/jumpserver/koko/pkg/lion/guacd"
	"github.com/jumpserver/koko/pkg/lion/session"
)

type clipboardTransfer struct {
	index string
	text  bool
	allow bool
	limit int
	size  int
}

const bytesPerMegabyte = 1024 * 1024

type clipboardPolicyFilter struct {
	perm     *session.ActionPermission
	toClient map[string]*clipboardTransfer
	toServer map[string]*clipboardTransfer
}

func newClipboardPolicyFilter(perm *session.ActionPermission) *clipboardPolicyFilter {
	return &clipboardPolicyFilter{
		perm:     perm,
		toClient: map[string]*clipboardTransfer{},
		toServer: map[string]*clipboardTransfer{},
	}
}

func (f *clipboardPolicyFilter) filterToClient(instruction *guacd.Instruction) *guacd.Instruction {
	return f.filter(instruction, f.toClient, true)
}

func (f *clipboardPolicyFilter) filterToServer(instruction *guacd.Instruction) *guacd.Instruction {
	return f.filter(instruction, f.toServer, false)
}

func (f *clipboardPolicyFilter) streamPolicy(toClient, text bool) (bool, int) {
	if f == nil || f.perm == nil {
		return true, 0
	}
	enabled := f.perm.EnablePaste
	if toClient {
		enabled = f.perm.EnableCopy
	}
	if f.perm.ClipboardPolicy == nil {
		return enabled, 0
	}
	item := f.perm.ClipboardPolicy.Paste
	if toClient {
		item = f.perm.ClipboardPolicy.Copy
	}
	if item == nil {
		return enabled, 0
	}
	if text {
		return enabled, item.TextLimit
	}
	return enabled, fileSizeLimitBytes(item.FileSizeLimit)
}

func fileSizeLimitBytes(limitMB int) int {
	if limitMB <= 0 {
		return 0
	}
	if limitMB > math.MaxInt/bytesPerMegabyte {
		return math.MaxInt
	}
	return limitMB * bytesPerMegabyte
}

func (f *clipboardPolicyFilter) filter(instruction *guacd.Instruction,
	transfers map[string]*clipboardTransfer, toClient bool) *guacd.Instruction {
	if f == nil || f.perm == nil || instruction == nil {
		return instruction
	}
	switch instruction.Opcode {
	case guacd.InstructionStreamingClipboard:
		if len(instruction.Args) < 2 {
			return instruction
		}
		text := strings.HasPrefix(instruction.Args[1], "text/")
		allow, limit := f.streamPolicy(toClient, text)
		transfer := &clipboardTransfer{
			index: instruction.Args[0],
			text:  text,
			allow: allow,
			limit: limit,
		}
		transfers[transfer.index] = transfer
		if !transfer.allow {
			return nil
		}
	case guacd.InstructionStreamingBlob:
		if len(instruction.Args) < 2 {
			return instruction
		}
		transfer := transfers[instruction.Args[0]]
		if transfer == nil {
			return instruction
		}
		if !transfer.allow {
			return nil
		}
		if transfer.limit > 0 {
			blob, err := base64.StdEncoding.DecodeString(instruction.Args[1])
			if err != nil {
				transfer.allow = false
				return nil
			}
			if transfer.text {
				transfer.size += utf8.RuneCount(blob)
			} else {
				transfer.size += len(blob)
			}
			if transfer.size > transfer.limit {
				transfer.allow = false
				return nil
			}
		}
	case guacd.InstructionStreamingEnd:
		if len(instruction.Args) > 0 {
			transfer := transfers[instruction.Args[0]]
			delete(transfers, instruction.Args[0])
			if transfer != nil && !transfer.allow {
				return nil
			}
		}
	}
	return instruction
}

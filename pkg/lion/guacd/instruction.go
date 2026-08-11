package guacd

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

type Instruction struct {
	Opcode       string
	Args         []string
	ProtocolForm string
}

func NewInstruction(opcode string, args ...string) (ret Instruction) {
	ret.Opcode = opcode
	ret.Args = args
	return ret
}

// 构造 `OPCODE,ARG1,ARG2,ARG3,...;` 的格式
func (opt *Instruction) String() string {
	if len(opt.ProtocolForm) > 0 {
		return opt.ProtocolForm
	}
	opt.ProtocolForm = fmt.Sprintf("%d.%s", utf8.RuneCountInString(opt.Opcode), opt.Opcode)
	for _, value := range opt.Args {
		opt.ProtocolForm += fmt.Sprintf(",%d.%s", utf8.RuneCountInString(value), value)
	}
	opt.ProtocolForm += semicolonDelimiter
	return opt.ProtocolForm
}

const (
	semicolonDelimiter = ";"
)

const (
	ByteDotDelimiter       = '.'
	ByteCommaDelimiter     = ','
	ByteSemicolonDelimiter = ';'
)

var (
	ErrInstructionMissSemicolon   = errors.New("instruction without semicolon")
	ErrInstructionMissDot         = errors.New("instruction without dot")
	ErrInstructionBadDigit        = errors.New("instruction with bad digit")
	ErrInstructionBadContent      = errors.New("instruction with bad Content")
	ErrInstructionBadTerminator   = errors.New("instruction with bad element terminator")
	ErrInstructionTooLong         = errors.New("instruction exceeds size limit")
	ErrInstructionTooManyElements = errors.New("instruction has too many elements")
	ErrInstructionInvalidUTF8     = errors.New("instruction contains invalid UTF-8")
	ErrInstructionTrailingData    = errors.New("data follows complete instruction")
)

// raw 是以 `;` 为结束符的原生字符串
func ParseInstructionString(raw string) (ret Instruction, err error) {
	if !strings.HasSuffix(raw, semicolonDelimiter) {
		return Instruction{}, fmt.Errorf("%w: %s", ErrInstructionMissSemicolon, raw)
	}

	decoder := NewInstructionDecoder(strings.NewReader(raw))
	ret, err = decoder.ReadInstruction()
	if err != nil {
		return Instruction{}, err
	}

	// ParseInstructionString represents exactly one instruction. Accepting a
	// second instruction here would silently reinterpret its opcode as an
	// argument of the first instruction.
	if _, nextErr := decoder.ReadInstruction(); nextErr == nil {
		return Instruction{}, fmt.Errorf("%w: multiple instructions", ErrInstructionTrailingData)
	} else if !errors.Is(nextErr, io.EOF) {
		return Instruction{}, fmt.Errorf("%w: %v", ErrInstructionTrailingData, nextErr)
	}

	return ret, nil
}

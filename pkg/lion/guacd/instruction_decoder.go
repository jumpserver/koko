package guacd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// These limits mirror the limits used by libguac's instruction parser. The
// byte limit accounts for the worst-case UTF-8 representation of the maximum
// number of Unicode codepoints.
const (
	maxInstructionElementCodepoints = 8192
	maxInstructionLengthDigits      = 5
	maxInstructionElements          = 128
	maxInstructionBytes             = maxInstructionElementCodepoints * utf8.UTFMax
)

// InstructionDecoder incrementally reads Guacamole instructions from a
// stream. A decoder must be reused when the supplied reader is not already
// buffered, as it may buffer bytes belonging to the next instruction.
type InstructionDecoder struct {
	reader *bufio.Reader
}

func NewInstructionDecoder(reader io.Reader) *InstructionDecoder {
	if buffered, ok := reader.(*bufio.Reader); ok {
		return &InstructionDecoder{reader: buffered}
	}
	return &InstructionDecoder{reader: bufio.NewReader(reader)}
}

// ReadInstruction reads exactly one instruction according to the Guacamole
// grammar:
//
//	LENGTH.VALUE[,LENGTH.VALUE...];
//
// Lengths count Unicode codepoints rather than UTF-8 bytes. io.EOF is returned
// only when no byte of a new instruction has been read. A truncated instruction
// returns an error that matches io.ErrUnexpectedEOF.
func (d *InstructionDecoder) ReadInstruction() (Instruction, error) {
	elements := make([]string, 0, 8)
	bytesRead := 0

	for {
		if len(elements) >= maxInstructionElements {
			return Instruction{}, fmt.Errorf("%w: maximum is %d",
				ErrInstructionTooManyElements, maxInstructionElements)
		}

		elementLength, err := d.readElementLength(&bytesRead)
		if err != nil {
			return Instruction{}, err
		}

		element, err := d.readElement(elementLength, &bytesRead)
		if err != nil {
			return Instruction{}, err
		}
		elements = append(elements, element)

		terminator, err := d.readByte(&bytesRead)
		if err != nil {
			return Instruction{}, wrapUnexpectedEOF(ErrInstructionBadTerminator, err)
		}

		switch terminator {
		case ByteCommaDelimiter:
			continue
		case ByteSemicolonDelimiter:
			if len(elements) == 1 {
				return NewInstruction(elements[0]), nil
			}
			return NewInstruction(elements[0], elements[1:]...), nil
		default:
			return Instruction{}, fmt.Errorf("%w: got %q",
				ErrInstructionBadTerminator, terminator)
		}
	}
}

func (d *InstructionDecoder) readElementLength(bytesRead *int) (int, error) {
	length := 0
	digits := 0

	for {
		current, err := d.readByte(bytesRead)
		if err != nil {
			// An entirely empty stream is the normal end-of-stream condition.
			if errors.Is(err, io.EOF) && *bytesRead == 0 {
				return 0, io.EOF
			}
			return 0, wrapUnexpectedEOF(ErrInstructionMissDot, err)
		}

		if current == ByteDotDelimiter {
			if digits == 0 {
				return 0, fmt.Errorf("%w: empty length prefix", ErrInstructionBadDigit)
			}
			return length, nil
		}

		if current == ByteCommaDelimiter || current == ByteSemicolonDelimiter {
			return 0, fmt.Errorf("%w: got %q", ErrInstructionMissDot, current)
		}
		if current < '0' || current > '9' {
			return 0, fmt.Errorf("%w: got %q", ErrInstructionBadDigit, current)
		}

		digits++
		if digits > maxInstructionLengthDigits {
			return 0, fmt.Errorf("%w: length prefix exceeds %d digits",
				ErrInstructionTooLong, maxInstructionLengthDigits)
		}

		length = length*10 + int(current-'0')
		if length > maxInstructionElementCodepoints {
			return 0, fmt.Errorf("%w: element length %d exceeds %d codepoints",
				ErrInstructionTooLong, length, maxInstructionElementCodepoints)
		}
	}
}

func (d *InstructionDecoder) readElement(length int, bytesRead *int) (string, error) {
	var element strings.Builder
	element.Grow(length)

	for range length {
		current, size, err := d.reader.ReadRune()
		if err != nil {
			return "", wrapUnexpectedEOF(ErrInstructionBadContent,
				normalizeReadError(err, *bytesRead))
		}

		*bytesRead += size
		if *bytesRead > maxInstructionBytes {
			return "", fmt.Errorf("%w: maximum is %d bytes",
				ErrInstructionTooLong, maxInstructionBytes)
		}
		if current == utf8.RuneError && size == 1 {
			return "", ErrInstructionInvalidUTF8
		}
		element.WriteRune(current)
	}

	return element.String(), nil
}

func (d *InstructionDecoder) readByte(bytesRead *int) (byte, error) {
	current, err := d.reader.ReadByte()
	if err != nil {
		return 0, normalizeReadError(err, *bytesRead)
	}

	*bytesRead++
	if *bytesRead > maxInstructionBytes {
		return 0, fmt.Errorf("%w: maximum is %d bytes",
			ErrInstructionTooLong, maxInstructionBytes)
	}
	return current, nil
}

func normalizeReadError(err error, bytesRead int) error {
	if errors.Is(err, io.EOF) && bytesRead > 0 {
		return io.ErrUnexpectedEOF
	}
	return err
}

func wrapUnexpectedEOF(parseErr, readErr error) error {
	if errors.Is(readErr, io.ErrUnexpectedEOF) {
		return fmt.Errorf("%w: %w", parseErr, readErr)
	}
	return readErr
}

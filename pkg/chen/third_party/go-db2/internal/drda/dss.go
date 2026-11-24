package drda

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// DSS Magic and Constants
const (
	DSSMagic byte = 0xD0 // DRDA Data Stream Structure Magic Identifier

	// DSS Types (low 4 bits of flag byte)
	DSSTypeRequest uint8 = 1 // Request DSS (RQSDSS)
	DSSTypeReply   uint8 = 2 // Reply DSS (RPYDSS)
	DSSTypeObject  uint8 = 3 // Object DSS (OBJDSS)
	DSSTypeComm    uint8 = 4 // Communications DSS

	// DSS Flag bitmasks
	DSSFlagChained uint8 = 0x40 // Bit 6: Chained to next DSS packet (0b01000000)
	DSSFlagError   uint8 = 0x20 // Bit 5: DSS Error (0b00100000)
	DSSFlagSameID  uint8 = 0x10 // Bit 4: Next DSS has same correlation ID (0b00010000)

	// MaxDSSPayloadSize defines the cumulative maximum allowed payload size for a DSS packet (128 MiB).
	MaxDSSPayloadSize = 128 * 1024 * 1024
)

var (
	// ErrInvalidDSSMagic is returned when the DSS header does not begin with 0xD0.
	ErrInvalidDSSMagic = errors.New("invalid DSS packet: missing 0xD0 magic identifier")
	// ErrTruncatedDSS is returned when fewer bytes than expected are read from the stream.
	ErrTruncatedDSS = errors.New("truncated DSS packet received")
	// ErrDSSPayloadTooLarge is returned when a DSS packet (single or continued) exceeds MaxDSSPayloadSize.
	ErrDSSPayloadTooLarge = errors.New("db2: DSS packet payload exceeds maximum limit of 128MB")
)

// DSSHeader represents the 6-byte DRDA Data Stream Structure header.
type DSSHeader struct {
	Length        uint16 // Total DSS length including the 6-byte header
	Type          uint8  // DSS Type (1=Request, 2=Reply, 3=Object)
	Chained       bool   // True if chained to a subsequent DSS
	SameID        bool   // True if next DSS shares the same correlation ID
	HasError      bool   // True if error flag is set
	CorrelationID uint16 // Correlation identifier
}

// BuildDSSHeader constructs a 6-byte DSS header.
func BuildDSSHeader(payloadLen int, dssType uint8, chained, sameID, hasError bool, correlationID uint16) []byte {
	totalLen := uint16(payloadLen + 6)
	hdr := make([]byte, 6)
	binary.BigEndian.PutUint16(hdr[0:2], totalLen)
	hdr[2] = DSSMagic

	flags := dssType & 0x0F
	if chained {
		flags |= DSSFlagChained
	}
	if sameID {
		flags |= DSSFlagSameID
	}
	if hasError {
		flags |= DSSFlagError
	}
	hdr[3] = flags
	binary.BigEndian.PutUint16(hdr[4:6], correlationID)
	return hdr
}

// WriteRequestDSS writes a single request DSS packet containing a DDM payload to the writer.
// It automatically determines whether the payload should be tagged as Request DSS or Object DSS.
// Returns the next correlation ID to use.
func WriteRequestDSS(w io.Writer, payload []byte, curID uint16, nextHasSameID, lastPacket bool) (uint16, error) {
	if len(payload) < 4 {
		return curID, errors.New("payload too short for DDM object")
	}

	codePoint := CodePoint(binary.BigEndian.Uint16(payload[2:4]))
	dssType := DSSTypeRequest
	if codePoint == CodePointSQLSTT || codePoint == CodePointSQLATTR || codePoint == CodePointSQLDTA || codePoint == CodePointEXTDTA {
		dssType = DSSTypeObject
	}

	// A DSS is at most 32767 bytes. A longer payload is sent as one DSS
	// continued over segments, the way ReadDSS reads it: the first segment has
	// the 6-byte header, every later one a 2-byte length, and the high bit of
	// a segment's length says another segment follows.
	first := payload
	rest := []byte(nil)
	if 6+len(payload) > maxDSSSegment {
		first, rest = payload[:maxDSSSegment-6], payload[maxDSSSegment-6:]
	}

	var hdr [6]byte
	length := uint16(6 + len(first))
	if len(rest) > 0 {
		length |= dssContinuationFlag
	}
	binary.BigEndian.PutUint16(hdr[0:2], length)
	hdr[2] = DSSMagic
	flags := dssType & 0x0F
	if !lastPacket {
		flags |= DSSFlagChained
	}
	if nextHasSameID {
		flags |= DSSFlagSameID
	}
	hdr[3] = flags
	binary.BigEndian.PutUint16(hdr[4:6], curID)

	if _, err := w.Write(hdr[:]); err != nil {
		return curID, fmt.Errorf("failed to write DSS header: %w", err)
	}
	if _, err := w.Write(first); err != nil {
		return curID, fmt.Errorf("failed to write DSS payload: %w", err)
	}

	for len(rest) > 0 {
		seg := rest
		if 2+len(seg) > maxDSSSegment {
			seg = rest[:maxDSSSegment-2]
		}
		rest = rest[len(seg):]
		segLen := uint16(2 + len(seg))
		if len(rest) > 0 {
			segLen |= dssContinuationFlag
		}
		binary.BigEndian.PutUint16(hdr[0:2], segLen)
		if _, err := w.Write(hdr[0:2]); err != nil {
			return curID, fmt.Errorf("failed to write DSS continuation header: %w", err)
		}
		if _, err := w.Write(seg); err != nil {
			return curID, fmt.Errorf("failed to write DSS continuation: %w", err)
		}
	}

	if !nextHasSameID {
		curID++
	}

	return curID, nil
}

// ReadDSS reads a complete DSS packet from the reader.
// Returns the DSS header, the outer DDM codepoint, the payload bytes, a boolean indicating if more query data pages follow, and an error.
//
// A DSS longer than 32767 bytes is split into segments: the high bit of the
// length marks a DSS as continued, and every continuation segment starts with
// its own 2-byte length carrying the same flag. All segments are read, so the
// next DSS always starts on a header. The DDM object inside may use an extended
// length (see readDDMObject). The more-data flag is always false: the payload
// is returned whole.
func ReadDSS(r io.Reader) (*DSSHeader, CodePoint, []byte, bool, error) {
	var hdrBuf [6]byte
	if _, err := io.ReadFull(r, hdrBuf[:]); err != nil {
		return nil, 0, nil, false, err
	}

	if hdrBuf[2] != DSSMagic {
		return nil, 0, nil, false, fmt.Errorf("%w: got 0x%02X", ErrInvalidDSSMagic, hdrBuf[2])
	}

	dssLen := binary.BigEndian.Uint16(hdrBuf[0:2])
	flags := hdrBuf[3]
	header := &DSSHeader{
		Length:        dssLen,
		Type:          flags & 0x0F,
		Chained:       (flags & DSSFlagChained) != 0,
		SameID:        (flags & DSSFlagSameID) != 0,
		HasError:      (flags & DSSFlagError) != 0,
		CorrelationID: binary.BigEndian.Uint16(hdrBuf[4:6]),
	}

	continued := dssLen&dssContinuationFlag != 0
	segLen := int(dssLen &^ dssContinuationFlag)
	if segLen < 6 {
		return header, 0, nil, false, fmt.Errorf("db2: invalid DSS frame length %d: less than header size 6", segLen)
	}

	body := make([]byte, segLen-6)
	if _, err := io.ReadFull(r, body); err != nil {
		return header, 0, nil, false, fmt.Errorf("failed to read DSS segment: %w", err)
	}

	for continued {
		var lenBuf [2]byte
		if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
			return header, 0, nil, false, fmt.Errorf("failed to read DSS continuation header: %w", err)
		}
		contLen := binary.BigEndian.Uint16(lenBuf[:])
		continued = contLen&dssContinuationFlag != 0
		n := int(contLen &^ dssContinuationFlag)
		if n < 2 {
			return header, 0, nil, false, fmt.Errorf("db2: invalid DSS continuation length %d", n)
		}
		segPayload := n - 2
		if int64(len(body))+int64(segPayload) > MaxDSSPayloadSize {
			return header, 0, nil, false, ErrDSSPayloadTooLarge
		}
		start := len(body)
		body = append(body, make([]byte, segPayload)...)
		if _, err := io.ReadFull(r, body[start:]); err != nil {
			return header, 0, nil, false, fmt.Errorf("failed to read DSS continuation: %w", err)
		}
	}

	codePoint, payload, err := readDDMObject(body)
	return header, codePoint, payload, false, err
}

// dssContinuationFlag is the high bit of a DSS or continuation segment length.
const dssContinuationFlag = 0x8000

// maxDSSSegment is the longest a DSS or one of its continuation segments can be.
const maxDSSSegment = 0x7FFF

// readDDMObject decodes the DDM object at the start of a DSS body.
//
// A DDM length with the high bit set is an extended length: the low 15 bits
// minus the 4-byte header give how many bytes of length follow the codepoint.
// Zero such bytes (0x8004) means the object runs to the end of the DSS, which
// is how Db2 sends XML values in EXTDTA.
func readDDMObject(body []byte) (CodePoint, []byte, error) {
	if len(body) < 4 {
		return 0, nil, fmt.Errorf("invalid DDM object length: DSS body holds %d bytes", len(body))
	}
	objLen := binary.BigEndian.Uint16(body[0:2])
	codePoint := CodePoint(binary.BigEndian.Uint16(body[2:4]))

	if objLen&0x8000 == 0 {
		if objLen < 4 {
			return codePoint, nil, fmt.Errorf("invalid DDM object length: %d", objLen)
		}
		if int(objLen) > len(body) {
			return codePoint, nil, fmt.Errorf("db2: DDM object length %d exceeds DSS frame payload capacity %d", objLen, len(body))
		}
		return codePoint, body[4:objLen], nil
	}

	extBytes := int(objLen&0x7FFF) - 4
	switch {
	case extBytes == 0:
		return codePoint, body[4:], nil
	case extBytes < 0 || extBytes > 8 || 4+extBytes > len(body):
		return codePoint, nil, fmt.Errorf("db2: invalid DDM extended length field 0x%04X", objLen)
	}
	var dataLen uint64
	for _, b := range body[4 : 4+extBytes] {
		dataLen = dataLen<<8 | uint64(b)
	}
	if dataLen > uint64(len(body)-4-extBytes) {
		return codePoint, nil, fmt.Errorf("db2: DDM extended length %d exceeds DSS payload %d", dataLen, len(body)-4-extBytes)
	}
	start := 4 + extBytes
	return codePoint, body[start : start+int(dataLen)], nil
}

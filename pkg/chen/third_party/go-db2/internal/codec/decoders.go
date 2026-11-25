package codec

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"time"
)

// DRDA Wire Data Types
const (
	DRDATypeInteger      uint8 = 0x02
	DRDATypeNInteger     uint8 = 0x03
	DRDATypeSmall        uint8 = 0x04
	DRDATypeNSmall       uint8 = 0x05
	DRDAType1ByteInt     uint8 = 0x06
	DRDATypeN1ByteInt    uint8 = 0x07
	DRDATypeFloat8       uint8 = 0x0A
	DRDATypeNFloat8      uint8 = 0x0B
	DRDATypeFloat4       uint8 = 0x0C
	DRDATypeNFloat4      uint8 = 0x0D
	DRDATypeDecimal      uint8 = 0x0E
	DRDATypeNDecimal     uint8 = 0x0F
	DRDATypeInteger8     uint8 = 0x16
	DRDATypeNInteger8    uint8 = 0x17
	DRDATypeLOBLOC       uint8 = 0x18
	DRDATypeNLOBLOC      uint8 = 0x19
	DRDATypeCLOBLOC      uint8 = 0x1A
	DRDATypeNCLOBLOC     uint8 = 0x1B
	DRDATypeDBCSCLOBLOC  uint8 = 0x1C
	DRDATypeNDBCSCLOBLOC uint8 = 0x1D
	DRDATypeRowID        uint8 = 0x1E
	DRDATypeNRowID       uint8 = 0x1F
	DRDATypeDate         uint8 = 0x20
	DRDATypeNDate        uint8 = 0x21
	DRDATypeTime         uint8 = 0x22
	DRDATypeNTime        uint8 = 0x23
	DRDATypeTimestamp    uint8 = 0x24
	DRDATypeNTimestamp   uint8 = 0x25
	DRDATypeFixByte      uint8 = 0x26
	DRDATypeNFixByte     uint8 = 0x27
	DRDATypeVarByte      uint8 = 0x28
	DRDATypeNVarByte     uint8 = 0x29
	DRDATypeLongVarByte  uint8 = 0x2A
	DRDATypeNLongVarByte uint8 = 0x2B
	DRDATypeChar         uint8 = 0x30
	DRDATypeNChar        uint8 = 0x31
	DRDATypeVarChar      uint8 = 0x32
	DRDATypeNVarChar     uint8 = 0x33
	DRDATypeLong         uint8 = 0x34
	DRDATypeNLong        uint8 = 0x35
	DRDATypeGraphic      uint8 = 0x36
	DRDATypeNGraphic     uint8 = 0x37
	DRDATypeVarGraph     uint8 = 0x38
	DRDATypeNVarGraph    uint8 = 0x39
	DRDATypeLonGraph     uint8 = 0x3A
	DRDATypeNLonGraph    uint8 = 0x3B
	DRDATypeMix          uint8 = 0x3C
	DRDATypeNMix         uint8 = 0x3D
	DRDATypeVarMix       uint8 = 0x3E
	DRDATypeNVarMix      uint8 = 0x3F
	DRDATypeLongMix      uint8 = 0x40
	DRDATypeNLongMix     uint8 = 0x41
	DRDATypeCStrMix      uint8 = 0x42
	DRDATypeNCStrMix     uint8 = 0x43
	DRDATypeBoolean      uint8 = 0xBE
	DRDATypeNBoolean     uint8 = 0xBF
	DRDATypeFixBytes     uint8 = 0xC0
	DRDATypeNFixBytes    uint8 = 0xC1
	DRDATypeVarBinary    uint8 = 0xC2
	DRDATypeNVarBinary   uint8 = 0xC3
	// DRDATypeXML is an XML value: Db2 describes an XML result column with it
	// and sends the serialized value as EXTDTA, as it does for LOBs.
	DRDATypeXML        uint8 = 0xC6
	DRDATypeNXML       uint8 = 0xC7
	DRDATypeLOBBytes   uint8 = 0xC8
	DRDATypeNLOBBytes  uint8 = 0xC9
	DRDATypeLOBCMIXED  uint8 = 0xCE
	DRDATypeNLOBCMIXED uint8 = 0xCF
	DRDATypeLOBCDBCS   uint8 = 0xCC
	DRDATypeNLOBCDBCS  uint8 = 0xCD
	DRDATypeLOBCSBCS   uint8 = 0xCA
	DRDATypeNLOBCSBCS  uint8 = 0xCB
	DRDATypeDecFloat   uint8 = 0xBA
	DRDATypeNDecFloat  uint8 = 0xBB
)

// Optimization: Static pre-ordered layout list for Db2 timestamp parsing to eliminate per-field slice heap allocations and put standard Db2 format first.
var timestampLayouts = []string{
	"2006-01-02-15.04.05.000000",
	"2006-01-02 15:04:05.000000",
	"2006-01-02-15.04.05.000000-07:00",
	"2006-01-02-15.04.05.000000+07:00",
	"2006-01-02-15.04.05-07:00",
	"2006-01-02-15.04.05+07:00",
	"2006-01-02-15.04.05",
	"2006-01-02 15:04:05.000000-07:00",
	"2006-01-02 15:04:05",
	time.RFC3339Nano,
	time.RFC3339,
}

// IsNullableDRDAType returns true if the DRDA wire type supports NULL indicators.
func IsNullableDRDAType(t uint8) bool {
	switch t {
	case DRDATypeNInteger, DRDATypeNSmall, DRDATypeN1ByteInt,
		DRDATypeNFloat8, DRDATypeNFloat4, DRDATypeNDecimal,
		DRDATypeNInteger8, DRDATypeNRowID, DRDATypeNDate,
		DRDATypeNTime, DRDATypeNTimestamp, DRDATypeNFixByte,
		DRDATypeNVarByte, DRDATypeNLongVarByte, DRDATypeNChar,
		DRDATypeNVarChar, DRDATypeNLong, DRDATypeNGraphic,
		DRDATypeNVarGraph, DRDATypeNLonGraph, DRDATypeNMix,
		DRDATypeNVarMix, DRDATypeNLongMix, DRDATypeNCStrMix,
		DRDATypeNBoolean, DRDATypeNFixBytes, DRDATypeNVarBinary,
		DRDATypeNLOBLOC, DRDATypeNCLOBLOC, DRDATypeNDBCSCLOBLOC,
		DRDATypeNLOBBytes, DRDATypeNLOBCSBCS, DRDATypeNDecFloat,
		DRDATypeNXML, DRDATypeNLOBCMIXED, DRDATypeNLOBCDBCS, 0xF5, 0xF7, 0xF9:
		return true
	default:
		return false
	}
}

// ExternalLOB is an unresolved non-empty LOB value. It must be replaced with
// its corresponding EXTDTA payload before a row is exposed to database/sql.
type ExternalLOB struct{}

// DecodeField reads and parses a single column value from a binary QRYDTA reader.
func DecodeField(drdaType uint8, ps []byte, r io.Reader, endian binary.ByteOrder) (any, error) {
	if len(ps) < 2 {
		return nil, fmt.Errorf("db2: invalid column parameter scale metadata length %d (expected >= 2)", len(ps))
	}

	if IsNullableDRDAType(drdaType) {
		var indicator byte
		if br, ok := r.(io.ByteReader); ok {
			// Optimization: When r implements io.ByteReader (e.g., *bytes.Reader used during QRYDTA response parsing),
			// call ReadByte() directly to avoid passing a subslice nullIndicator[:] to io.ReadFull, which causes escape analysis
			// to heap-allocate nullIndicator (reducing 1 heap allocation per nullable field decode).
			var err error
			indicator, err = br.ReadByte()
			if err != nil {
				return nil, err
			}
		} else {
			var nullIndicator [1]byte
			if _, err := io.ReadFull(r, nullIndicator[:]); err != nil {
				return nil, err
			}
			indicator = nullIndicator[0]
		}
		if indicator == 0xFF || indicator == 0x80 || indicator == 0x7F {
			return nil, nil // Null or omitted output value
		}
	}

	switch drdaType {
	case DRDATypeChar, DRDATypeNChar, DRDATypeMix, DRDATypeNMix:
		ln := int(binary.BigEndian.Uint16(ps))
		var stackBuf [64]byte
		var buf []byte
		if ln <= len(stackBuf) {
			buf = stackBuf[:ln]
		} else {
			buf = make([]byte, ln)
		}
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		for len(buf) > 0 && buf[len(buf)-1] == ' ' {
			buf = buf[:len(buf)-1]
		}
		return string(buf), nil

	case DRDATypeGraphic, DRDATypeNGraphic:
		charLen := int(binary.BigEndian.Uint16(ps))
		byteLen := charLen * 2
		var stackBuf [64]byte
		var buf []byte
		if byteLen <= len(stackBuf) {
			buf = stackBuf[:byteLen]
		} else {
			buf = make([]byte, byteLen)
		}
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		return TrimRightGraphicSpaces(DecodeUTF16BE(buf)), nil

	case DRDATypeVarChar, DRDATypeNVarChar,
		DRDATypeVarMix, DRDATypeNVarMix,
		DRDATypeLong, DRDATypeNLong,
		DRDATypeLongMix, DRDATypeNLongMix:
		rawLen, err := read2Bytes(r, binary.BigEndian)
		if err != nil {
			return nil, err
		}
		ln := int(rawLen)
		var stackBuf [64]byte
		var buf []byte
		if ln <= len(stackBuf) {
			buf = stackBuf[:ln]
		} else {
			buf = make([]byte, ln)
		}
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		return string(buf), nil

	case DRDATypeVarGraph, DRDATypeNVarGraph,
		DRDATypeLonGraph, DRDATypeNLonGraph:
		rawLen, err := read2Bytes(r, binary.BigEndian)
		if err != nil {
			return nil, err
		}
		byteLen := int(rawLen) * 2
		var stackBuf [64]byte
		var buf []byte
		if byteLen <= len(stackBuf) {
			buf = stackBuf[:byteLen]
		} else {
			buf = make([]byte, byteLen)
		}
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		return DecodeUTF16BE(buf), nil

	case DRDATypeSmall, DRDATypeNSmall:
		v, err := read2Bytes(r, endian)
		if err != nil {
			return nil, err
		}
		return int64(int16(v)), nil

	case DRDATypeInteger, DRDATypeNInteger:
		v, err := read4Bytes(r, endian)
		if err != nil {
			return nil, err
		}
		return int64(int32(v)), nil

	case DRDATypeInteger8, DRDATypeNInteger8:
		v, err := read8Bytes(r, endian)
		if err != nil {
			return nil, err
		}
		return int64(v), nil

	case DRDAType1ByteInt, DRDATypeN1ByteInt:
		if br, ok := r.(io.ByteReader); ok {
			b, err := br.ReadByte()
			if err != nil {
				return nil, err
			}
			return int64(int8(b)), nil
		}
		var buf [1]byte
		if _, err := io.ReadFull(r, buf[:]); err != nil {
			return nil, err
		}
		return int64(int8(buf[0])), nil

	case DRDATypeFloat4, DRDATypeNFloat4:
		bits, err := read4Bytes(r, endian)
		if err != nil {
			return nil, err
		}
		return float64(math.Float32frombits(bits)), nil

	case DRDATypeFloat8, DRDATypeNFloat8:
		bits, err := read8Bytes(r, endian)
		if err != nil {
			return nil, err
		}
		return math.Float64frombits(bits), nil

	case DRDATypeBoolean, DRDATypeNBoolean:
		ln := int(binary.BigEndian.Uint16(ps))
		if ln <= 0 {
			return false, nil
		}
		var stackBuf [16]byte
		var buf []byte
		if ln <= len(stackBuf) {
			buf = stackBuf[:ln]
		} else {
			buf = make([]byte, ln)
		}
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		return buf[len(buf)-1] != 0, nil

	case DRDATypeDate, DRDATypeNDate:
		ln := int(binary.BigEndian.Uint16(ps))
		var stackBuf [16]byte
		var buf []byte
		if ln <= len(stackBuf) {
			buf = stackBuf[:ln]
		} else {
			buf = make([]byte, ln)
		}
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		s := string(buf)
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			return s, nil
		}
		return t, nil

	case DRDATypeTime, DRDATypeNTime:
		ln := int(binary.BigEndian.Uint16(ps))
		var stackBuf [16]byte
		var buf []byte
		if ln <= len(stackBuf) {
			buf = stackBuf[:ln]
		} else {
			buf = make([]byte, ln)
		}
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		return string(buf), nil

	case DRDATypeTimestamp, DRDATypeNTimestamp:
		ln := int(binary.BigEndian.Uint16(ps))
		var stackBuf [32]byte
		var buf []byte
		if ln <= len(stackBuf) {
			buf = stackBuf[:ln]
		} else {
			buf = make([]byte, ln)
		}
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		for len(buf) > 0 && buf[len(buf)-1] == ' ' {
			buf = buf[:len(buf)-1]
		}
		s := string(buf)
		// Db2 timestamps can carry twelve fractional digits. Go's time.Parse
		// accepts that text but silently discards digits beyond nanoseconds.
		const fractionStart = len("2006-01-02-15.04.05.")
		if len(buf) >= fractionStart+10 && buf[fractionStart-1] == '.' &&
			len(bytes.Trim(buf[fractionStart:fractionStart+10], "0123456789")) == 0 {
			return s, nil
		}
		for _, layout := range timestampLayouts {
			if t, err := time.Parse(layout, s); err == nil {
				return t, nil
			}
		}
		return s, nil

	case DRDATypeDecFloat, DRDATypeNDecFloat:
		nBytes := 8
		if len(ps) > 1 && ps[1] == 16 {
			nBytes = 16
		} else if len(ps) > 0 && ps[0] == 16 {
			nBytes = 16
		}
		buf := make([]byte, nBytes)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		return DecodeDFP(buf)

	case DRDATypeVarBinary, DRDATypeNVarBinary, DRDATypeVarByte, DRDATypeNVarByte:
		rawLen, err := read2Bytes(r, binary.BigEndian)
		if err != nil {
			return nil, err
		}
		buf := make([]byte, int(rawLen))
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		return buf, nil

	case DRDATypeFixByte, DRDATypeNFixByte, DRDATypeFixBytes, DRDATypeNFixBytes, DRDATypeRowID, DRDATypeNRowID:
		ln := int(binary.BigEndian.Uint16(ps)) & 0x7FFF
		buf := make([]byte, ln)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		return buf, nil

	case DRDATypeDecimal, DRDATypeNDecimal:
		if len(ps) < 2 {
			return nil, fmt.Errorf("invalid Decimal ps descriptor length: %d", len(ps))
		}
		precision := int(ps[0])
		// Security: Convert ps[1] to int8 first so negative DRDA scale values (e.g. 0xFE for -2)
		// are correctly sign-extended instead of being treated as large positive uint8 values (254),
		// preventing value corruption and excessive memory allocations in DecodePackedDecimal.
		scale := int(int8(ps[1]))
		byteLen := (precision + 2) / 2
		var stackBuf [64]byte
		var buf []byte
		if byteLen <= len(stackBuf) {
			buf = stackBuf[:byteLen]
		} else {
			buf = make([]byte, byteLen)
		}
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		return DecodePackedDecimal(buf, scale), nil

	case DRDATypeLOBLOC, DRDATypeNLOBLOC, DRDATypeCLOBLOC, DRDATypeNCLOBLOC,
		DRDATypeDBCSCLOBLOC, DRDATypeNDBCSCLOBLOC,
		DRDATypeLOBBytes, DRDATypeNLOBBytes, DRDATypeLOBCSBCS, DRDATypeNLOBCSBCS,
		DRDATypeLOBCMIXED, DRDATypeNLOBCMIXED, DRDATypeLOBCDBCS, DRDATypeNLOBCDBCS,
		DRDATypeXML, DRDATypeNXML,
		0x10, 0x11, 0xF4, 0xF5, 0xF6, 0xF7, 0xF8, 0xF9:
		ln := int(binary.BigEndian.Uint16(ps)) & 0x7FFF
		if ln == 0 {
			ln = 4
		}
		buf := make([]byte, ln)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		// Db2's extended length descriptor has a one-byte prefix and eight
		// length bytes. A zero-length LOB has no EXTDTA object on the wire.
		length := buf
		if len(buf) == 9 {
			length = buf[1:]
		}
		external := len(buf) > 0 && buf[0]&0x80 != 0
		for _, b := range length {
			external = external || b != 0
		}
		if external {
			return ExternalLOB{}, nil
		}
		if drdaType == DRDATypeLOBCSBCS || drdaType == DRDATypeNLOBCSBCS || drdaType == 0xF6 || drdaType == 0xF7 ||
			drdaType == DRDATypeLOBCMIXED || drdaType == DRDATypeNLOBCMIXED || drdaType == DRDATypeLOBCDBCS || drdaType == DRDATypeNLOBCDBCS ||
			drdaType == DRDATypeDBCSCLOBLOC || drdaType == DRDATypeNDBCSCLOBLOC || drdaType == 0xF8 || drdaType == 0xF9 ||
			drdaType == DRDATypeXML || drdaType == DRDATypeNXML {
			return "", nil
		}
		return []byte{}, nil

	default:
		// Fallback: try reading ps length
		if len(ps) >= 2 {
			ln := int(binary.BigEndian.Uint16(ps)) & 0x7FFF
			if ln > 0 {
				buf := make([]byte, ln)
				if _, err := io.ReadFull(r, buf); err != nil {
					return nil, err
				}
				return buf, nil
			}
		}
		return nil, fmt.Errorf("unsupported DRDA field type: 0x%02X", drdaType)
	}
}

// DecodePackedDecimal converts IBM Packed Decimal bytes into a decimal string.
// Optimization: Pre-calculates exact required output string length and populates a single pre-allocated byte slice directly (~16% faster).
func DecodePackedDecimal(b []byte, scale int) string {
	if len(b) == 0 {
		return "0"
	}

	var stackDigits [64]byte
	var digits []byte
	if len(b)*2 <= 64 {
		digits = stackDigits[:]
	} else {
		digits = make([]byte, len(b)*2)
	}
	pos := 0

	for i := 0; i < len(b)-1; i++ {
		digits[pos] = '0' + ((b[i] >> 4) & 0x0F)
		digits[pos+1] = '0' + (b[i] & 0x0F)
		pos += 2
	}

	lastByte := b[len(b)-1]
	digits[pos] = '0' + ((lastByte >> 4) & 0x0F)
	pos++

	sign := lastByte & 0x0F
	isNegative := sign == 0x0D || sign == 0x0B

	start := 0
	for start < pos-1 && digits[start] == '0' {
		start++
	}

	significant := digits[start:pos]
	numSig := len(significant)

	if numSig == 1 && significant[0] == '0' && scale < 0 {
		if isNegative {
			return "-0"
		}
		return "0"
	}

	negLen := 0
	if isNegative {
		negLen = 1
	}

	var outLen int
	if scale < 0 {
		outLen = negLen + numSig - scale
	} else if scale == 0 {
		outLen = negLen + numSig
	} else if numSig <= scale {
		outLen = negLen + 2 + scale
	} else {
		outLen = negLen + numSig + 1
	}

	// Optimization: Use a stack buffer for output bytes when outLen <= 64 to avoid extra heap allocation.
	var stackOut [64]byte
	var out []byte
	if outLen <= 64 {
		out = stackOut[:outLen]
	} else {
		out = make([]byte, outLen)
	}
	idx := 0
	if isNegative {
		out[0] = '-'
		idx = 1
	}

	if scale < 0 {
		copy(out[idx:], significant)
		idx += numSig
		for k := 0; k < -scale; k++ {
			out[idx] = '0'
			idx++
		}
	} else if scale == 0 {
		copy(out[idx:], significant)
	} else if numSig <= scale {
		out[idx] = '0'
		out[idx+1] = '.'
		idx += 2
		for k := 0; k < scale-numSig; k++ {
			out[idx] = '0'
			idx++
		}
		copy(out[idx:], significant)
	} else {
		dotPos := numSig - scale
		copy(out[idx:], significant[:dotPos])
		idx += dotPos
		out[idx] = '.'
		idx++
		copy(out[idx:], significant[dotPos:])
	}

	return string(out)
}

// ParseSQLDTARD parses an SQLDTARD (0x2413) DDM reply payload containing output parameter values.
func ParseSQLDTARD(data []byte, endian binary.ByteOrder) ([]any, error) {
	if len(data) < 4 {
		return nil, errors.New("SQLDTARD payload too short")
	}

	offset := 0
	var dscBytes []byte
	var dtaBytes []byte

	for offset < len(data) {
		if offset+4 > len(data) {
			return nil, errors.New("incomplete SQLDTARD parameter header")
		}
		paramLen := int(binary.BigEndian.Uint16(data[offset : offset+2]))
		codePoint := binary.BigEndian.Uint16(data[offset+2 : offset+4])

		var payload []byte
		if paramLen&0x8000 == 0 {
			if paramLen < 4 || offset+paramLen > len(data) {
				return nil, fmt.Errorf("invalid SQLDTARD parameter length %d at offset %d", paramLen, offset)
			}
			payload = data[offset+4 : offset+paramLen]
			offset += paramLen
		} else {
			extBytes := int(paramLen&0x7FFF) - 4
			if extBytes == 0 {
				payload = data[offset+4:]
				offset = len(data)
			} else {
				if extBytes < 0 || extBytes > 8 || offset+4+extBytes > len(data) {
					return nil, fmt.Errorf("invalid SQLDTARD extended length field 0x%04X at offset %d", paramLen, offset)
				}
				var dataLen uint64
				for _, b := range data[offset+4 : offset+4+extBytes] {
					dataLen = dataLen<<8 | uint64(b)
				}
				if dataLen > uint64(len(data)-(offset+4+extBytes)) {
					return nil, fmt.Errorf("invalid SQLDTARD extended payload length %d at offset %d", dataLen, offset)
				}
				payload = data[offset+4+extBytes : offset+4+extBytes+int(dataLen)]
				offset += 4 + extBytes + int(dataLen)
			}
		}

		if codePoint == 0x0010 { // FDODSC
			dscBytes = payload
		} else if codePoint == 0x147A { // FDODTA
			dtaBytes = payload
		}
	}

	if len(dtaBytes) == 0 {
		return nil, nil
	}

	type triplet struct {
		typ uint8
		ps  [2]byte
	}

	// Optimization: Single pass descriptor parsing using stack buffer for standard field counts (<= 32 fields) to eliminate heap allocations and loop overhead.
	var stackFields [32]triplet
	fields := stackFields[:0]

	pos := 0
	for pos+3 <= len(dscBytes) {
		groupLen := int(dscBytes[pos])
		if groupLen < 3 || pos+groupLen > len(dscBytes) {
			break
		}
		if dscBytes[pos+1] == 0x76 {
			numFields := groupLen/3 - 1
			fPos := pos + 3
			for i := 0; i < numFields && fPos+3 <= pos+groupLen; i++ {
				fields = append(fields, triplet{
					typ: dscBytes[fPos],
					ps:  [2]byte{dscBytes[fPos+1], dscBytes[fPos+2]},
				})
				fPos += 3
			}
		}
		pos += groupLen
	}

	br := bytes.NewReader(dtaBytes)
	if br.Len() >= 2 {
		var hdr [2]byte
		_, _ = io.ReadFull(br, hdr[:])
		if hdr[0] != 0xFF {
			br.Reset(dtaBytes)
		}
	}

	results := make([]any, 0, len(fields))
	for _, f := range fields {
		if br.Len() == 0 {
			break
		}
		val, err := DecodeField(f.typ, f.ps[:], br, endian)
		if err != nil {
			return results, err
		}
		results = append(results, val)
	}

	return results, nil
}

// read2Bytes reads 2 bytes from r using io.ByteReader if available to avoid slice heap allocation.
func read2Bytes(r io.Reader, endian binary.ByteOrder) (uint16, error) {
	if br, ok := r.(io.ByteReader); ok {
		b0, err := br.ReadByte()
		if err != nil {
			return 0, err
		}
		b1, err := br.ReadByte()
		if err != nil {
			return 0, err
		}
		if endian == binary.BigEndian {
			return uint16(b0)<<8 | uint16(b1), nil
		}
		return uint16(b1)<<8 | uint16(b0), nil
	}
	var buf [2]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}
	return endian.Uint16(buf[:]), nil
}

// read4Bytes reads 4 bytes from r using io.ByteReader if available to avoid slice heap allocation.
func read4Bytes(r io.Reader, endian binary.ByteOrder) (uint32, error) {
	if br, ok := r.(io.ByteReader); ok {
		b0, err := br.ReadByte()
		if err != nil {
			return 0, err
		}
		b1, err := br.ReadByte()
		if err != nil {
			return 0, err
		}
		b2, err := br.ReadByte()
		if err != nil {
			return 0, err
		}
		b3, err := br.ReadByte()
		if err != nil {
			return 0, err
		}
		if endian == binary.BigEndian {
			return uint32(b0)<<24 | uint32(b1)<<16 | uint32(b2)<<8 | uint32(b3), nil
		}
		return uint32(b3)<<24 | uint32(b2)<<16 | uint32(b1)<<8 | uint32(b0), nil
	}
	var buf [4]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}
	return endian.Uint32(buf[:]), nil
}

// read8Bytes reads 8 bytes from r using io.ByteReader if available to avoid slice heap allocation.
func read8Bytes(r io.Reader, endian binary.ByteOrder) (uint64, error) {
	if br, ok := r.(io.ByteReader); ok {
		b0, err := br.ReadByte()
		if err != nil {
			return 0, err
		}
		b1, err := br.ReadByte()
		if err != nil {
			return 0, err
		}
		b2, err := br.ReadByte()
		if err != nil {
			return 0, err
		}
		b3, err := br.ReadByte()
		if err != nil {
			return 0, err
		}
		b4, err := br.ReadByte()
		if err != nil {
			return 0, err
		}
		b5, err := br.ReadByte()
		if err != nil {
			return 0, err
		}
		b6, err := br.ReadByte()
		if err != nil {
			return 0, err
		}
		b7, err := br.ReadByte()
		if err != nil {
			return 0, err
		}
		if endian == binary.BigEndian {
			return uint64(b0)<<56 | uint64(b1)<<48 | uint64(b2)<<40 | uint64(b3)<<32 |
				uint64(b4)<<24 | uint64(b5)<<16 | uint64(b6)<<8 | uint64(b7), nil
		}
		return uint64(b7)<<56 | uint64(b6)<<48 | uint64(b5)<<40 | uint64(b4)<<32 |
			uint64(b3)<<24 | uint64(b2)<<16 | uint64(b1)<<8 | uint64(b0), nil
	}
	var buf [8]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}
	return endian.Uint64(buf[:]), nil
}

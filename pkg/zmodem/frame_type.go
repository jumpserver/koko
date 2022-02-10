package zmodem

//FRAME TYPES
const (
	ZRQINIT    = 0x00 /* request receive init (s->r) */
	ZRINIT     = 0x01 /* receive init (r->s) */
	ZSINIT     = 0x02 /* send init sequence (optional) (s->r) */
	ZACK       = 0x03 /* ack to ZRQINIT ZRINIT or ZSINIT (s<->r) */
	ZFILE      = 0x04 /* file name (s->r) */
	ZSKIP      = 0x05 /* skip this file (r->s) */
	ZNAK       = 0x06 /* last packet was corrupted (?) */
	ZABORT     = 0x07 /* abort batch transfers (?) */
	ZFIN       = 0x08 /* finish session (s<->r) */
	ZRPOS      = 0x09 /* resume data transmission here (r->s) */
	ZDATA      = 0x0a /* data packet(s) follow (s->r) */
	ZEOF       = 0x0b /* end of file reached (s->r) */
	ZFERR      = 0x0c /* fatal read or write error detected (?) */
	ZCRC       = 0x0d /* request for file CRC and response (?) */
	ZCHALLENGE = 0x0e /* security challenge (r->s) */
	ZCOMPL     = 0x0f /* request is complete (?) */
	ZCAN       = 0x10 /* pseudo frame; other end cancelled session with 5* CAN */
	ZFREECNT   = 0x11 /* request free bytes on file system (s->r) */
	ZCOMMAND   = 0x12 /* issue command (s->r) */
	ZSTDERR    = 0x13 /* output data to stderr (??) */
)

type FrameType byte

func (t FrameType) String() string {
	switch t {
	case ZRQINIT:
		return "ZRQINIT"
	case ZRINIT:
		return "ZRINIT"
	case ZSINIT:
		return "ZSINIT"
	case ZACK:
		return "ZACK"
	case ZFILE:
		return "ZFILE"
	case ZSKIP:
		return "ZSKIP"
	case ZNAK:
		return "ZNAK"
	case ZABORT:
		return "ZABORT"
	case ZFIN:
		return "ZFIN"
	case ZRPOS:
		return "ZRPOS"
	case ZDATA:
		return "ZDATA"
	case ZEOF:
		return "ZEOF"
	case ZFERR:
		return "ZFERR"
	case ZCRC:
		return "ZCRC"
	case ZCHALLENGE:
		return "ZCHALLENGE"
	case ZCOMPL:
		return "ZCOMPL"
	case ZCAN:
		return "ZCAN"
	case ZFREECNT:
		return "ZFREECNT"
	case ZCOMMAND:
		return "ZCOMMAND"
	case ZSTDERR:
		return "ZSTDERR"
	}
	return "UNKNOWN"
}

const (
	ZDLE = 0x18 /* ctrl-x zmodem escape */

	// ZPAD *
	ZPAD = 0x2a /* pad character; begins frames */

	// ZBIN A
	ZBIN = 0x41 /* binary frame indicator (CRC16) */

	// ZHEX B
	ZHEX = 0x42 /* hex frame indicator */

	// ZBIN32 C
	ZBIN32 = 0x43 /* binary frame indicator (CRC32) */

	CAN = 0x18
)

//ZDLE SEQUENCES
const (
	ZCRCE = 0x68 /* CRC next, frame ends, header packet follows */
	ZCRCG = 0x69 /* CRC next, frame continues nonstop */
	ZCRCQ = 0x6a /* CRC next, frame continuous, ZACK expected */
	ZCRCW = 0x6b /* CRC next, ZACK expected, end of frame */

	ZRUB0 = 0x6c /* translate to rubout 0x7f */
	ZRUB1 = 0x6d /* translate to rubout 0xff */
)

var (
	HexHeaderPrefix = []byte{ZPAD, ZPAD, ZDLE, ZHEX}

	Binary16HeaderPrefix = []byte{ZPAD, ZDLE, ZBIN}

	Binary32HeaderPrefix = []byte{ZPAD, ZDLE, ZBIN32}

	AbortSession = []byte{CAN, CAN, CAN, CAN, CAN}

	CancelSequence = []byte{CAN, CAN, CAN, CAN, CAN, CAN, CAN, CAN, CAN, CAN,
		0x08, 0x08, 0x08, 0x08, 0x08, 0x08, 0x08, 0x08, 0x08, 0x08}
)

var SkipSequence = []byte{
	0x2a, 0x2a, 0x18, 0x42,
	0x30, 0x35, 0x30, 0x30,
	0x30, 0x30, 0x30, 0x30,
	0x36, 0x33, 0x37, 0x66,
	0x39, 0x32, 0x0d, 0x8a, 0x11,
}

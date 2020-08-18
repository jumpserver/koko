package telnetlib

import (
	"regexp"
)

const (
	IAC  = 255 // "Interpret As Command"
	DONT = 254
	DO   = 253
	WONT = 252
	WILL = 251
)

const (
	SE = 240 // Subnegotiation End

	NOP = 241 // No Operation
	DM  = 242 // Data Mark
	BRK = 243 // Break
	IP  = 244 // Interrupt process
	AO  = 245 // Abort output
	AYT = 246 // Are You There
	EC  = 247 // Erase Character
	EL  = 248 // Erase Line
	GA  = 249 // Go Ahead

	SB = 250 // Subnegotiation Begin
)

const (
	BINARY = 0  // 8-bit data path
	ECHO   = 1  // echo
	RCP    = 2  // prepare to reconnect
	SGA    = 3  // suppress go ahead
	NAMS   = 4  // approximate message size
	STATUS = 5  // give status
	TM     = 6  // timing mark
	RCTE   = 7  // remote controlled transmission and echo
	NAOL   = 8  // negotiate about output line width
	NAOP   = 9  // negotiate about output page size
	NAOCRD = 10 // negotiate about CR disposition
	NAOHTS = 11 // negotiate about horizontal tabstops
	NAOHTD = 12 // negotiate about horizontal tab disposition
	NAOFFD = 13 // negotiate about formfeed disposition
	NAOVTS = 14 // negotiate about vertical tab stops
	NAOVTD = 15 // negotiate about vertical tab disposition
	NAOLFD = 16 // negotiate about output LF disposition
	XASCII = 17 // extended ascii character set
	LOGOUT = 18 // force logout

	BM  = 19 // byte macro
	DET = 20 // data entry terminal

	SUPDUP       = 21 // supdup protocol
	SUPDUPOUTPUT = 22 // supdup output

	SNDLOC = 23 // send location
	TTYPE  = 24 // terminal type
	EOR    = 25 // end or record

	TUID = 26 // TACACS user identification

	OUTMRK = 27 // output marking

	TTYLOC       = 28 // terminal location number
	VT3270REGIME = 29 // 3270 regime
	X3PAD        = 30 // X.3 PAD

	NAWS = 31 // window size

	TSPEED         = 32 // terminal speed
	LFLOW          = 33 // remote flow control
	LINEMODE       = 34 // Linemode option
	XDISPLOC       = 35 // X Display Location
	OLD_ENVIRON    = 36 // Old - Environment variables
	AUTHENTICATION = 37 // Authenticate
	ENCRYPT        = 38 // Encryption option
	NEW_ENVIRON    = 39 // New - Environment variables
)

var CodeTOASCII = map[byte]string{
	IAC:            "IAC",
	WILL:           "WILL",
	WONT:           "WONT",
	DO:             "DO",
	DONT:           "DONT",
	SE:             "SE",
	SB:             "SB",
	BINARY:         "BINARY",
	ECHO:           "ECHO",
	RCP:            "RCP",
	SGA:            "SGA",
	NAMS:           "NAMS",
	STATUS:         "STATUS ",
	TM:             "TM",
	RCTE:           "RCTE",
	NAOL:           "NAOL",
	NAOP:           "NAOP",
	NAOCRD:         "NAOCRD",
	NAOHTS:         "NAOHTS",
	NAOHTD:         "NAOHTD",
	NAOFFD:         "NAOFFD",
	NAOVTS:         "NAOVTS",
	NAOVTD:         "NAOVTD",
	NAOLFD:         "NAOLFD",
	XASCII:         "XASCII",
	LOGOUT:         "LOGOUT",
	BM:             "BM",
	DET:            "DET",
	SUPDUP:         "SUPDUP",
	SUPDUPOUTPUT:   "SUPDUPOUTPUT",
	SNDLOC:         "SNDLOC",
	TTYPE:          "TTYPE",
	EOR:            "EOR",
	TUID:           "TUID",
	OUTMRK:         "OUTMRK",
	TTYLOC:         "TTYLOC",
	VT3270REGIME:   "VT3270REGIME",
	X3PAD:          "X3PAD",
	NAWS:           "NAWS",
	TSPEED:         "TSPEED",
	LFLOW:          "LFLOW",
	LINEMODE:       "LINEMODE",
	XDISPLOC:       "XDISPLOC",
	OLD_ENVIRON:    "OLD_ENVIRON",
	AUTHENTICATION: "AUTHENTICATION",
	ENCRYPT:        "ENCRYPT",
	NEW_ENVIRON:    "NEW_ENVIRON",
}

const (
	loginRegs          = "(?i)login:?\\s*$|username:?\\s*$|name:?\\s*$|用户名:?\\s*$|账\\s*号:?\\s*$|user:?\\s*$"
	passwordRegs       = "(?i)Password:?\\s*$|ssword:?\\s*$|passwd:?\\s*$|密\\s*码:?\\s*$"
	FailedRegs         = "(?i)incorrect|failed|失败|错误"
	DefaultSuccessRegs = "(?i)Last\\s*login|success|成功|#|>|\\$"
)

var (
	incorrectPattern, _ = regexp.Compile(FailedRegs)
	usernamePattern, _  = regexp.Compile(loginRegs)
	passwordPattern, _  = regexp.Compile(passwordRegs)
	successPattern, _   = regexp.Compile(DefaultSuccessRegs)
)

type AuthStatus int

const (
	AuthSuccess AuthStatus = iota
	AuthPartial
	AuthFailed
)

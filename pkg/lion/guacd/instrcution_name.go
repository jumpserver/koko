package guacd

// Streaming instructions
const (
	InstructionStreamingAck       = "ack"
	InstructionStreamingArgv      = "argv"
	InstructionStreamingAudio     = "audio"
	InstructionStreamingBlob      = "blob"
	InstructionStreamingClipboard = "clipboard"
	InstructionStreamingEnd       = "end"
	InstructionStreamingFile      = "file"
	InstructionStreamingImg       = "img"
	InstructionStreamingNest      = "nest"
	InstructionStreamingPipe      = "pipe"
	InstructionStreamingVideo     = "video"
)

// Object instructions
const (
	InstructionObjectBody       = "body"
	InstructionObjectFilesystem = "filesystem"
	InstructionObjectGet        = "get"
	InstructionObjectPut        = "put"
	InstructionObjectUndefine   = "undefine"
)

// Client handshake instructions
const (
	InstructionClientHandshakeAudio    = "audio"
	InstructionClientHandshakeConnect  = "connect"
	InstructionClientHandshakeImage    = "image"
	InstructionClientHandshakeSelect   = "select"
	InstructionClientHandshakeSize     = "size"
	InstructionClientHandshakeTimezone = "timezone"
	InstructionClientHandshakeVideo    = "video"
)

// Server handshake instructions
const (
	InstructionServerHandshakeArgs = "args"
)

// Client control instructions
const (
	InstructionClientDisconnect = "disconnect"
	InstructionClientNop        = "nop"
	InstructionClientSync       = "sync"
)

// Server control instructions
const (
	InstructionServerDisconnect = "disconnect"
	InstructionServerError      = "error"
	InstructionServerLog        = "log"
	InstructionServerMouse      = "mouse"
)

// Client events
const (
	InstructionKey   = "key"
	InstructionMouse = "mouse"
	InstructionSize  = "size"
)

const (
	InstructionRequired = "required"
)

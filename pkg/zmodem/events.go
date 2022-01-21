package zmodem

type StatusEvent string

const (
	StartEvent StatusEvent = "ZMODEM_START"

	EndEvent StatusEvent = "ZMODEM_END"
)

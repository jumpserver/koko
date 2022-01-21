package exchange

import "encoding/json"

type RoomMessage struct {
	Event string `json:"event"`
	Body  []byte `json:"data"`

	Meta MetaMessage `json:"meta"` // receive的信息必须携带Meta
}

type MetaMessage struct {
	UserId     string `json:"user_id"`
	User       string `json:"user"`
	Created    string `json:"created"`
	RemoteAddr string `json:"remote_addr"`
}

func (m RoomMessage) Marshal() []byte {
	p, _ := json.Marshal(m)
	return p
}

const (
	PingEvent    = "Ping"
	DataEvent    = "Data"
	WindowsEvent = "Windows"

	JoinEvent  = "Join"
	LeaveEvent = "Leave"

	ExitEvent = "Exit"

	JoinSuccessEvent = "JoinSuccess"

	ShareJoin  = "Share_JOIN"
	ShareLeave = "Share_LEAVE"
	ShareUsers = "Share_USERS"

	ActionEvent = "Action"
)

const (
	ZmodemStartEvent = "ZMODEM_START"
	ZmodemEndEvent   = "ZMODEM_END"
)

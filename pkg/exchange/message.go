package exchange

import "encoding/json"

type RoomMessage struct {
	Event string `json:"event"`
	Body  []byte `json:"data"`

	Meta MetaMessage `json:"meta"` // receive的信息必须携带Meta
}

func (m *RoomMessage) Marshal() []byte {
	p, _ := json.Marshal(m)
	return p
}

type MetaMessage struct {
	UserId     string `json:"user_id"`
	User       string `json:"user"`
	Created    string `json:"created"`
	RemoteAddr string `json:"remote_addr"`

	TerminalId string `json:"terminal_id"`
	Primary    bool   `json:"primary"`
	Writable   bool   `json:"writable"`
}

const (
	PingEvent    = "Ping"
	DataEvent    = "Data"
	WindowsEvent = "Windows"

	PauseEvent  = "Pause"
	ResumeEvent = "Resume"

	JoinEvent  = "Join"
	LeaveEvent = "Leave"

	ExitEvent = "Exit"

	JoinSuccessEvent = "JoinSuccess"

	ShareJoin  = "Share_JOIN"
	ShareLeave = "Share_LEAVE"
	ShareUsers = "Share_USERS"

	ActionEvent = "Action"

	ShareRemoveUser = "Share_REMOVE_USER"
)

const (
	ZmodemStartEvent = "ZMODEM_START"
	ZmodemEndEvent   = "ZMODEM_END"
)

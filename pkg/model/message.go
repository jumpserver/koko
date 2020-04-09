package model

import "encoding/json"

type RoomMessage struct {
	Event string `json:"event"`
	Body  []byte `json:"data"`
}

func (m RoomMessage) Marshal() []byte {
	p, _ := json.Marshal(m)
	return p
}

func (m RoomMessage) UnMarshal(p interface{}) {
	_ = json.Unmarshal(m.Body, p)
}

const (
	PingEvent    = "Ping"
	DataEvent    = "Data"
	WindowsEvent = "Windows"

	MaxIdleEvent        = "MaxIdle"   // 退出
	ExitEvent           = "Exit"      // 退出
	LogoutEvent         = "Logout"    // 退出
	AdminTerminateEvent = "Terminate" //退出

)

package tunnel

type MetaShareUserMessage struct {
	ShareId string `json:"share_id"`

	SessionId  string `json:"session_id"`
	UserId     string `json:"user_id"`
	User       string `json:"user"`
	Created    string `json:"created"`
	RemoteAddr string `json:"remote_addr"`
	Primary    bool   `json:"primary"`
	Writable   bool   `json:"writable"`
}

type SessionRoomMessage struct {
	Id        string `json:"id"`
	SessionId string `json:"session_id"`
	Event     *Event `json:"event"`
}

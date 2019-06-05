package httpd

type HostMsg struct {
	Uuid   string `json:"uuid"`
	UserID string `json:"userid"`
	Secret string `json:"secret"`
	Size   []int  `json:"size"`
}

type ResizeMsg struct {
	Height int `json:"rows"`
	Width  int `json:"cols"`
}

type TokenMsg struct {
	Token  string `json:"token"`
	Secret string `json:"secret"`
	Size   []int  `json:"size"`
}

type DataMsg struct {
	Data string `json:"data"`
	Room string `json:"room"`
}

type RoomMsg struct {
	Room   string `json:"room"`
	Secret string `json:"secret"`
}

type EmitDataMsg struct {
	Room string `json:"room"`
	Data string `json:"data"`
}

type EmitLogoutMsg struct {
	Room string `json:"room"`
}

type EmitDisconnectMsg struct {
}

type EmitSidMsg struct {
	Sid string `json:"sid"`
}

package httpd

import "time"

type Message struct {
	Id   string `json:"id"`
	Type string `json:"type"`
	Data string `json:"data"`
}

const (
	PING           = "PING"
	PONG           = "PONG"
	CONNECT        = "CONNECT"
	CLOSE          = "CLOSE"
	TERMINALINIT   = "TERMINAL_INIT"
	TERMINALDATA   = "TERMINAL_DATA"
	TERMINALRESIZE = "TERMINAL_RESIZE"
)

type WindowSize struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

const (
	ginCtxUserKey = "CtxUserKey"
)

const (
	TargetTypeAsset = "asset"
	TargetTypeDB    = "database_app"
	TargetTypeK8s   = "k8s_app"

	TargetTypeRoom = "shareroom"
)

const (
	maxReadTimeout  = 5 * time.Minute
	maxWriteTimeOut = 5 * time.Minute
)

const (
	AppTypeAsset = iota + 1
	AppTypeK8s
	AppTypeDB
	AppUnknown
)

const (
	TTYName       = "terminal"
	WebFolderName = "web_folder"
)

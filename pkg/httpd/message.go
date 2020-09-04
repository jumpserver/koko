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
	ginCtxUserKey       = "CtxUserKey"
	ginCtxTokenUserKey  = "CtxTokenUserKey"
	ginCtxElfinderIdKey = "CtxElfinderIdKey"
)

const (
	AssetTargetType = "asset"
	DbTargetType    = "database_app"
	K8sTargetType   = "k8s_app"

	TokenTargetType = "token"
	RoomTargetType     = "shareroom"
	ElfinderTargetType = "elfinder"

)

const (
	maxReadTimeout  = 5 * time.Minute
	maxWriteTimeOut = 5 * time.Minute
)

const (
	AssetAppType = "asset"
	K8sAppType   = "k8s_app"
	DBAppType    = "database_app"
)

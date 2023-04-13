package httpd

import (
	"github.com/jumpserver/koko/pkg/exchange"
	"time"

	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
)

type Message struct {
	Id   string `json:"id"`
	Type string `json:"type"`
	Data string `json:"data"`
	Raw  []byte `json:"-"`
	Err  string `json:"err"`
}

const (
	PING    = "PING"
	PONG    = "PONG"
	CONNECT = "CONNECT"
	CLOSE   = "CLOSE"
	ERROR   = "ERROR"

	TerminalInit    = "TERMINAL_INIT"
	TerminalData    = "TERMINAL_DATA"
	TerminalResize  = "TERMINAL_RESIZE"
	TerminalBinary  = "TERMINAL_BINARY"
	TerminalAction  = "TERMINAL_ACTION"
	TerminalSession = "TERMINAL_SESSION"

	TerminalShare        = "TERMINAL_SHARE"
	TerminalShareJoin    = "TERMINAL_SHARE_JOIN"
	TerminalShareLeave   = "TERMINAL_SHARE_LEAVE"
	TerminalShareUsers   = "TERMINAL_SHARE_USERS"
	TerminalGetShareUser = "TERMINAL_GET_SHARE_USER"

	TerminalShareUserRemove = "TERMINAL_SHARE_USER_REMOVE"

	TerminalError = "TERMINAL_ERROR"
)

type WindowSize struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

type TerminalConnectData struct {
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
	Code string `json:"code"`
}

type ShareRequestMeta struct {
	Users []string `json:"users"`
}

type ShareRequestParams struct {
	model.SharingSessionRequest
}

type GetUserParams struct {
	Query string `json:"query"`
}

type RemoveSharingUserParams struct {
	SessionId string               `json:"session"`
	UserMeta  exchange.MetaMessage `json:"user_meta"`
}

type ShareResponse struct {
	ShareId string `json:"share_id"`
	Code    string `json:"code"`
}

type ShareInfo struct {
	Record model.ShareRecord
}

const (
	TargetTypeMonitor = "monitor"

	TargetTypeShare = "share"
)

const (
	maxReadTimeout  = 5 * time.Minute
	maxWriteTimeOut = 5 * time.Minute
)

const (
	TTYName       = "terminal"
	WebFolderName = "web_folder"
)

type ViewPageMata struct {
	ID      string
	IconURL string
}

type WsRequestParams struct {
	TargetType string `form:"type"`
	TargetId   string `form:"target_id"`
	Token      string `form:"token"`

	AssetId string `form:"asset"`

	// k8s container
	Pod       string `form:"pod"`
	Namespace string `form:"namespace"`
	Container string `form:"container"`

	// mysql database
	DisableAutoHash string `form:"disableautohash"`
}

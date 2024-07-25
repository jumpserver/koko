package httpd

import (
	"time"

	"github.com/jumpserver/koko/pkg/exchange"

	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
)

type Message struct {
	Id   string `json:"id"`
	Type string `json:"type"`
	Data string `json:"data"`
	Raw  []byte `json:"raw"`
	Err  string `json:"err"`

	//Chat AI
	Prompt    string `json:"prompt"`
	Interrupt bool   `json:"interrupt"`

	//K8S
	KubernetesId string `json:"k8s_id"`
	Namespace    string `json:"namespace"`
	Pod          string `json:"pod"`
	Container    string `json:"container"`
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

	TerminalSessionPause  = "TERMINAL_SESSION_PAUSE"
	TerminalSessionResume = "TERMINAL_SESSION_RESUME"

	TerminalShare        = "TERMINAL_SHARE"
	TerminalShareJoin    = "TERMINAL_SHARE_JOIN"
	TerminalShareLeave   = "TERMINAL_SHARE_LEAVE"
	TerminalShareUsers   = "TERMINAL_SHARE_USERS"
	TerminalGetShareUser = "TERMINAL_GET_SHARE_USER"

	TerminalShareUserRemove = "TERMINAL_SHARE_USER_REMOVE"

	TerminalSyncUserPreference = "TERMINAL_SYNC_USER_PREFERENCE"

	TerminalError = "TERMINAL_ERROR"

	MessageNotify = "MESSAGE_NOTIFY"

	TerminalK8SInit   = "TERMINAL_K8S_INIT"
	TerminalK8STree   = "TERMINAL_K8S_TREE"
	TerminalK8SData   = "TERMINAL_K8S_DATA"
	TerminalK8SBinary = "TERMINAL_K8S_BINARY"
	TerminalK8SResize = "TERMINAL_K8S_RESIZE"
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

type UserKoKoPreferenceParam struct {
	ThemeName string `json:"terminal_theme_name"`
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
	ChatName      = "chat"
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

type OpenAIParam struct {
	AuthToken string
	BaseURL   string
	Proxy     string
	Model     string
	Prompt    string
}

type AIConversation struct {
	Id                   string
	Prompt               string
	HistoryRecords       []string
	InterruptCurrentChat bool
}

type ChatGPTMessage struct {
	ID         string    `json:"id"`
	Content    string    `json:"content"`
	CreateTime time.Time `json:"create_time,omitempty"`
	Type       string    `json:"type"`
	Role       string    `json:"role"`
}

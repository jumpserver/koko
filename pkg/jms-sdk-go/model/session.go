package model

import (
	"encoding/json"
	"strings"

	"github.com/jumpserver/koko/pkg/jms-sdk-go/common"
)

var (
	_ json.Unmarshaler = (*LabelField)(nil)
	_ json.Marshaler   = (*LabelField)(nil)
)

type LabelField string

func (s LabelField) MarshalJSON() ([]byte, error) {
	return []byte(`"` + s + `"`), nil
}

func (s *LabelField) UnmarshalJSON(bytes []byte) error {
	var labelValue struct {
		Label string `json:"label"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(bytes, &labelValue); err != nil {
		return err
	}
	*s = LabelField(labelValue.Value)
	return nil
}

const (
	NORMALType  LabelField = "normal"
	TUNNELType  LabelField = "tunnel"
	COMMANDType LabelField = "command"
	SFTPType    LabelField = "sftp"
)

const (
	LoginFromSSH LabelField = "ST"
	LoginFromWeb LabelField = "WT"
)

type Session struct {
	ID         string         `json:"id,omitempty"`
	User       string         `json:"user"`
	Asset      string         `json:"asset"`
	Account    string         `json:"account"`
	LoginFrom  LabelField     `json:"login_from,omitempty"`
	RemoteAddr string         `json:"remote_addr"`
	Protocol   string         `json:"protocol"`
	DateStart  common.UTCTime `json:"date_start"`
	OrgID      string         `json:"org_id"`
	UserID     string         `json:"user_id"`
	AssetID    string         `json:"asset_id"`
	AccountID  string         `json:"account_id"`
	Type       LabelField     `json:"type"`
	ErrReason  LabelField     `json:"error_reason,omitempty"`
}

type ReplayVersion string

const (
	UnKnown  ReplayVersion = ""
	Version2 ReplayVersion = "2"
	Version3 ReplayVersion = "3"
)

const (
	SuffixReplayGz = ".replay.gz"
	SuffixCastGz   = ".cast.gz"
	SuffixCast     = ".cast"
	SuffixGz       = ".gz"
)

var SuffixMap = map[ReplayVersion]string{
	Version2: SuffixReplayGz,
	Version3: SuffixCastGz,
}

func ParseReplayVersion(gzFile string, defaultValue ReplayVersion) ReplayVersion {
	for version, suffix := range SuffixMap {
		if strings.HasSuffix(gzFile, suffix) {
			return version
		}
	}
	return defaultValue
}

type ReplayError LabelField

func (r ReplayError) Error() string {
	return string(r)
}

const (
	SessionReplayErrConnectFailed ReplayError = "connect_failed"
	SessionReplayErrCreatedFailed ReplayError = "replay_create_failed"
	SessionReplayErrUploadFailed  ReplayError = "replay_upload_failed"
	SessionReplayErrUnsupported   ReplayError = "replay_unsupported"
)

type LifecycleEvent string

const (
	AssetConnectSuccess  LifecycleEvent = "asset_connect_success"
	AssetConnectFinished LifecycleEvent = "asset_connect_finished"
	CreateShareLink      LifecycleEvent = "create_share_link"
	UserJoinSession      LifecycleEvent = "user_join_session"
	UserLeaveSession     LifecycleEvent = "user_leave_session"
	AdminJoinMonitor     LifecycleEvent = "admin_join_monitor"
	AdminExitMonitor     LifecycleEvent = "admin_exit_monitor"
	ReplayConvertStart   LifecycleEvent = "replay_convert_start"
	ReplayConvertSuccess LifecycleEvent = "replay_convert_success"
	ReplayConvertFailure LifecycleEvent = "replay_convert_failure"
	ReplayUploadStart    LifecycleEvent = "replay_upload_start"
	ReplayUploadSuccess  LifecycleEvent = "replay_upload_success"
	ReplayUploadFailure  LifecycleEvent = "replay_upload_failure"
)

type SessionLifecycleLog struct {
	Reason string `json:"reason"`
	User   string `json:"user"`
}

var EmptyLifecycleLog = SessionLifecycleLog{}

type SessionLifecycleReasonErr string

const (
	ReasonErrConnectFailed     SessionLifecycleReasonErr = "connect_failed"
	ReasonErrConnectDisconnect SessionLifecycleReasonErr = "connect_disconnect"
	ReasonErrUserClose         SessionLifecycleReasonErr = "user_close"
	ReasonErrIdleDisconnect    SessionLifecycleReasonErr = "idle_disconnect"
	ReasonErrAdminTerminate    SessionLifecycleReasonErr = "admin_terminate"
	ReasonErrMaxSessionTimeout SessionLifecycleReasonErr = "max_session_timeout"
	ReasonErrPermissionExpired SessionLifecycleReasonErr = "permission_expired"
	ReasonErrNullStorage       SessionLifecycleReasonErr = "null_storage"
)

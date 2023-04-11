package model

type SharingSession struct {
	ID          string `json:"id"`
	IsActive    bool   `json:"is_active"`
	ExpiredTime int    `json:"expired_time"`
	Code        string `json:"verify_code"`

	ActionPermission string `json:"action_permission"`
}

type ShareRecord struct {
	ID      string   `json:"id"`
	Code    string   `json:"verify_code"`
	Session ObjectId `json:"session"`
	Sharing ObjectId `json:"sharing"`
	OrgId   string   `json:"org_id"`
	OrgName string   `json:"org_name"`

	ActionPermission LabelValue `json:"action_permission"`

	Err interface{} `json:"error"`
}

const (
	readOnlyPermission = "readonly"
	writablePermission = "writable"
)

func (s ShareRecord) Writeable() bool {
	return s.ActionPermission.Value == writablePermission
}

type ObjectId struct {
	ID string `json:"id"`
}

type SharingSessionRequest struct {
	SessionID  string   `json:"session"`
	ExpireTime int      `json:"expired_time"`
	Users      []string `json:"users"`
	ActionPerm string   `json:"action_permission"`
}

type SharePostData struct {
	ShareId    string `json:"sharing"`
	Code       string `json:"verify_code"`
	UserId     string `json:"joiner"`
	RemoteAddr string `json:"remote_addr"`
}

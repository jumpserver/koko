package model

type SharingSession struct {
	ID          string `json:"id"`
	IsActive    bool   `json:"is_active"`
	ExpiredTime int    `json:"expired_time"`
	Code        string `json:"verify_code"`

	ActionPermission int `json:"action_permission"`
}

type ShareRecord struct {
	ID      string   `json:"id"`
	Code    string   `json:"verify_code"`
	Session ObjectId `json:"session"`
	Sharing ObjectId `json:"sharing"`
	OrgId   string   `json:"org_id"`
	OrgName string   `json:"org_name"`

	ActionPermission struct {
		Label string `json:"label"`
		Value int    `json:"value"`
	} `json:"action_permission"`

	Err interface{} `json:"error"`
}

const (
	readOnlyPermission  = 0
	writeablePermission = 1
)

func (s ShareRecord) Writeable() bool {
	return s.ActionPermission.Value == writeablePermission
}

type ObjectId struct {
	ID string `json:"id"`
}

type SharingSessionRequest struct {
	SessionID  string   `json:"session"`
	ExpireTime int      `json:"expired_time"`
	Users      []string `json:"users"`
	ActionPerm int      `json:"action_permission"`
}

type SharePostData struct {
	ShareId    string `json:"sharing"`
	Code       string `json:"verify_code"`
	UserId     string `json:"joiner"`
	RemoteAddr string `json:"remote_addr"`
}

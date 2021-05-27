package model

import (
	"github.com/jumpserver/koko/pkg/jms-sdk-go/common"
)

type Session struct {
	ID string `json:"id"`
	// "%s(%s)" Name Username
	User         string         `json:"user"`
	Asset        string         `json:"asset"`
	SystemUser   string         `json:"system_user"`
	LoginFrom    string         `json:"login_from"`
	RemoteAddr   string         `json:"remote_addr"`
	Protocol     string         `json:"protocol"`
	DateStart    common.UTCTime `json:"date_start"`
	OrgID        string         `json:"org_id"`
	UserID       string         `json:"user_id"`
	AssetID      string         `json:"asset_id"`
	SystemUserID string         `json:"system_user_id"`
}

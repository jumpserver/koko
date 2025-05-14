package model

import "github.com/jumpserver/koko/pkg/jms-sdk-go/common"

type PublicSetting struct {
	Interface struct {
		LoginTitle string `json:"login_title"`
		LogOut     string `json:"logo_logout"`
		Index      string `json:"logo_index"`
		Image      string `json:"login_image"`
		Favicon    string `json:"favicon"`
	} `json:"INTERFACE"`
	EnableSessionShare bool `json:"SECURITY_SESSION_SHARE"`
	EnableAnnouncement bool `json:"ANNOUNCEMENT_ENABLED"`
	Announcement       struct {
		Id        string         `json:"ID"`
		Subject   string         `json:"SUBJECT"`
		Content   string         `json:"CONTENT"`
		Link      string         `json:"LINK"`
		DateStart common.UTCTime `json:"DATE_START"`
		DateEnd   common.UTCTime `json:"DATE_END"`
	} `json:"ANNOUNCEMENT"`
}

/*
{
	"WINDOWS_SKIP_ALL_MANUAL_PASSWORD": false,
	"SECURITY_MAX_IDLE_TIME": 3,
	"XPACK_ENABLED": true,
	"LOGIN_CONFIRM_ENABLE": true,
	"SECURITY_VIEW_AUTH_NEED_MFA": true,
	"SECURITY_MFA_VERIFY_TTL": 60,
	"OLD_PASSWORD_HISTORY_LIMIT_COUNT": 2,
	"SECURITY_COMMAND_EXECUTION": true,
	"SECURITY_PASSWORD_EXPIRATION_TIME": 10000,
	"SECURITY_LUNA_REMEMBER_AUTH": true,
	"XPACK_LICENSE_IS_VALID": true,
	"TICKETS_ENABLED": true,
	"PASSWORD_RULE": {
		"SECURITY_PASSWORD_MIN_LENGTH": 6,
		"SECURITY_ADMIN_USER_PASSWORD_MIN_LENGTH": 6,
		"SECURITY_PASSWORD_UPPER_CASE": false,
		"SECURITY_PASSWORD_LOWER_CASE": false,
		"SECURITY_PASSWORD_NUMBER": false,
		"SECURITY_PASSWORD_SPECIAL_CHAR": false
	},
	"AUTH_WECOM": true,
	"AUTH_DINGTALK": true,
	"AUTH_FEISHU": true,
	"SECURITY_WATERMARK_ENABLED": true,
	"SECURITY_SESSION_SHARE": true,
	"XRDP_ENABLED": true,
	INTERFACE: {
		logo_logout: "/static/img/logo.png",
		logo_index: "/static/img/logo_text_white.png",
		login_image: "/static/img/login_image.png",
		favicon: "/static/img/facio.ico",
		login_title: "JumpServer 开源堡垒机",
		theme: "classic_green",
		theme_info: { },
		beian_link: "",
		beian_text: ""
	}
}
*/

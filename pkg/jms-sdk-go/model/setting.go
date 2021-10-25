package model

type PublicSetting struct {
	LoginTitle string `json:"LOGIN_TITLE"`
	LogoURLS   struct {
		LogOut  string `json:"logo_logout"`
		Index   string `json:"logo_index"`
		Image   string `json:"login_image"`
		Favicon string `json:"favicon"`
	} `json:"LOGO_URLS"`
	EnableWatermark bool `json:"SECURITY_WATERMARK_ENABLED"`
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
	"LOGIN_TITLE": "欢迎使用JumpServer开源堡垒机",
	"LOGO_URLS": {
		"logo_logout": "/static/img/logo.png",
		"logo_index": "/static/img/logo_text.png",
		"login_image": "/static/img/login_image.jpg",
		"favicon": "/static/img/facio.ico"
	},
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
	"XRDP_ENABLED": true
}
*/

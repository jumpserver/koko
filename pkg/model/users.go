package model

/*
	{'id': '1f8e54a8-d99d-4074-b35d-45264adb4e34',
	'name': 'EricdeMBP.lan',
	'username': 'EricdeMBP.lan',
	'email': 'EricdeMBP.lan@serviceaccount.local',
	'groups': [],
	'groups_display': '',
	'role': 'App','role_display': '应用程序',
	'avatar_url': '/static/img/avatar/user.png',
	'wechat': '','phone': None, 'otp_level': 0,
	'comment': '', 'source': 'local',
	'source_display': 'Local',
	'is_valid': True, 'is_expired': False,
	'is_active': True, 'created_by': '',
	'is_first_login': True, 'date_password_last_updated': '2019-04-08 18:18:24 +0800',
	'date_expired': '2089-03-21 18:18:24 +0800'}
*/

type AuthResponse struct {
	Token string `json:"token"`
	Seed  string `json:"seed"`
	User  *User  `json:"user"`
}

type User struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Role     string `json:"role"`
	IsValid  bool   `json:"is_valid"`
	IsActive bool   `json:"is_active"`
	OTPLevel int    `json:"otp_level"`
}

type TokenUser struct {
	UserID         string `json:"user"`
	UserName       string `json:"username"`
	AssetID        string `json:"asset"`
	Hostname       string `json:"hostname"`
	SystemUserID   string `json:"system_user"`
	SystemUserName string `json:"system_user_name"`
}

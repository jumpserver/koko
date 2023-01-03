package model

type SharingSession struct {
	ID          string `json:"id"`
	IsActive    bool   `json:"is_active"`
	ExpiredTime int    `json:"expired_time"`
	Code        string `json:"verify_code"`
}

type ShareRecord struct {
	ID      string      `json:"id"`
	Code    string      `json:"verify_code"`
	Session ObjectId    `json:"session"`
	Sharing ObjectId    `json:"sharing"`
	OrgId   string      `json:"org_id"`
	OrgName string      `json:"org_name"`
	Err     interface{} `json:"error"`
}

type ObjectId struct {
	ID string `json:"id"`
}

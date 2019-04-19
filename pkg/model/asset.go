package model

type Asset struct {
	Id              string       `json:"id"`
	Hostname        string       `json:"hostname"`
	Ip              string       `json:"ip"`
	Port            int          `json:"port"`
	SystemUsers     []SystemUser `json:"system_users_granted"`
	IsActive        bool         `json:"is_active"`
	SystemUsersJoin string       `json:"system_users_join"`
	Os              string       `json:"os"`
	Domain          string       `json:"domain"`
	Platform        string       `json:"platform"`
	Comment         string       `json:"comment"`
	Protocol        string       `json:"protocol"`
	OrgID           string       `json:"org_id"`
	OrgName         string       `json:"org_name"`
}

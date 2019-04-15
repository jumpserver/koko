package model

/*
{
	"id": "135ce78d-c4fe-44ca-9be3-c86581cb4365",
	"hostname": "coco2",
	"ip": "127.0.0.1",
	"port": 32769,
	"system_users_granted": [{
		"id": "fbd39f8c-fa3e-4c2b-948e-ce1e0380b4f9",
		"name": "docker_root",
		"username": "root",
		"priority": 19,
		"protocol": "ssh",
		"comment": "screencast",
		"login_mode": "auto"
	}],
	"is_active": true,
	"system_users_join": "root",
	"os": null,
	"domain": null,
	"platform": "Linux",
	"comment": "",
	"protocol": "ssh",
	"org_id": "",
	"org_name": "DEFAULT"
}
*/

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

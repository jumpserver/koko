package model

type Ticket struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Action string `json:"action"`
	Status string `json:"status"`
	OrgID  string `json:"org_id"`
	OrgName string `json:"org_name"`
}

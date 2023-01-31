package model

type CommandTicketInfo struct {
	TicketInfo
}

type ReqInfo struct {
	Method string `json:"method"`
	URL    string `json:"url"`
}

type TicketState struct {
	ID        string     `json:"id"`
	Processor string     `json:"processor,omitempty"`
	State     LabelFiled `json:"state"`
	Status    LabelFiled `json:"status"`
}

const (
	TicketOpen     = "pending"
	TicketApproved = "approved"
	TicketRejected = "rejected"
	TicketClosed   = "closed"
)

type AssetLoginTicketInfo struct {
	TicketId    string `json:"ticket_id"`
	NeedConfirm bool   `json:"need_confirm"`
	TicketInfo
}

type TicketInfo struct {
	CheckReq        ReqInfo  `json:"check_ticket_api"`
	CloseReq        ReqInfo  `json:"close_ticket_api"`
	TicketDetailUrl string   `json:"ticket_detail_page_url"`
	Reviewers       []string `json:"assignees"`
}

package model

type CommandTicketInfo struct {
	TicketInfo
}

type ReqInfo struct {
	Method string `json:"method"`
	URL    string `json:"url"`
}

type TicketState struct {
	ID        string `json:"id"`
	Processor string `json:"processor,omitempty"`
	State     string `json:"state"`
	Status    string `json:"status"`
}

const (
	TicketOpen     = "open"
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
	CheckReq        ReqInfo  `json:"check_confirm_status"`
	CloseReq        ReqInfo  `json:"close_confirm"`
	TicketDetailUrl string   `json:"ticket_detail_url"`
	Reviewers       []string `json:"reviewers"`
}

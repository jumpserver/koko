package service

const (
	ErrLoginConfirmWait     = "login_confirm_wait"
	ErrLoginConfirmRejected = "login_confirm_rejected"
	ErrLoginConfirmRequired = "login_confirm_required"
	ErrMFARequired          = "mfa_required"
	ErrPasswordFailed       = "password_failed"
)

const (
	TicketStatusOpen   = "open"
	TicketStatusClosed = "closed"
)

const (
	TicketActionOpen    = "open"
	TicketActionClose   = "close"
	TicketActionApprove = "approve"
	TicketActionReject  = "reject"
)

package auth

var mfaInstruction = "Please enter 6 digits."
var mfaQuestion = "[MFA auth]: "

var confirmInstruction = "Please wait for your admin to confirm."
var confirmQuestion = "Do you want to continue [Y/n]? : "

const (
	actionAccepted        = "Accepted"
	actionFailed          = "Failed"
	actionPartialAccepted = "Partial accepted"
)
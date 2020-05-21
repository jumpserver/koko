package model

type contextKey int64

const (
	ContextKeyUser contextKey = iota + 1
	ContextKeyRemoteAddr
	ContextKeyClient
	ContextKeyConfirmRequired
	ContextKeyConfirmFailed
	ContextKeyCurrentLang
)

const (
	HighRiskFlag = "1"
	LessRiskFlag = "0"
)

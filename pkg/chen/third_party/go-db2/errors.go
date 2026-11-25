package db2

import "github.com/go-db2/go-db2/internal/drda"

// Error represents a structured error returned by the IBM Db2 database engine.
type Error = drda.Error

// Db2Error is kept as a type alias for backwards compatibility.
type Db2Error = Error

// Common Driver Sentinel Errors
var (
	ErrConnectionClosed     = drda.ErrConnectionClosed
	ErrDatabaseNotFound     = drda.ErrDatabaseNotFound
	ErrAuthenticationFailed = drda.ErrAuthenticationFailed
	ErrInvalidConnectionStr = drda.ErrInvalidConnectionStr
)

package drda

import (
	"errors"
	"fmt"
)

var (
	// ErrConnectionClosed is returned when an operation is attempted on a closed connection.
	ErrConnectionClosed = errors.New("db2: connection is closed")

	// ErrDatabaseNotFound is returned when the target relational database does not exist on the Db2 server.
	ErrDatabaseNotFound = errors.New("db2: relational database not found (RDBNFNRM)")

	// ErrAuthenticationFailed is returned when credentials or authentication handshake fails on Db2.
	ErrAuthenticationFailed = errors.New("db2: authentication failed (SECCHKRM)")

	// ErrInvalidConnectionStr is returned when the DSN format or parameter is invalid.
	ErrInvalidConnectionStr = errors.New("db2: invalid connection string")
)

// Error represents a structured error returned by the IBM Db2 database engine or DRDA protocol layer.
type Error struct {
	SQLCode  int32
	SQLState string
	Message  string
	cause    error
}

// NewError creates a new structured *Error.
func NewError(sqlCode int32, sqlState, message string, cause error) *Error {
	return &Error{
		SQLCode:  sqlCode,
		SQLState: sqlState,
		Message:  message,
		cause:    cause,
	}
}

// Error formats the structured Db2 error.
func (e *Error) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("db2: SQLCODE=%d, SQLSTATE=%s: %s", e.SQLCode, e.SQLState, e.Message)
	}
	return fmt.Sprintf("db2: SQLCODE=%d, SQLSTATE=%s", e.SQLCode, e.SQLState)
}

// Unwrap returns the underlying cause if present.
func (e *Error) Unwrap() error {
	return e.cause
}

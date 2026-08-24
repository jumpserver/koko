package terminalai

import (
	"context"
	"fmt"
)

type BackgroundExecutor interface {
	Execute(
		ctx context.Context,
		command string,
		onOutput func(string),
	) (string, *int, error)
	Close() error
}

type ProfileProvider interface {
	DetectProfile(context.Context) AssetProfile
}

type SQLMetadataProvider interface {
	SQLMetadataScope() string
	LookupSQLSchema(context.Context, SQLSchemaLookupRequest) (SQLSchemaLookupResult, error)
	InvalidateSQLMetadata()
}

type BackgroundUnavailableError struct {
	Cause error
}

func (e *BackgroundUnavailableError) Error() string {
	return fmt.Sprintf("background executor is unavailable: %s", e.Cause)
}

func (e *BackgroundUnavailableError) Unwrap() error {
	return e.Cause
}

package engine

import (
	"errors"
	"strings"

	db2 "github.com/go-db2/go-db2"
)

func db2ServerCancellation(err error) bool {
	var native *db2.Error
	return !db2UncertainNativeError(err) && errors.As(err, &native) && (native.SQLState == "57014" || native.SQLState == "HY008")
}

// Transport failures/timeouts cannot prove a dispatched write was rejected.
func db2UncertainNativeError(err error) bool {
	var native *db2.Error
	if !errors.As(err, &native) {
		return false
	}
	state := native.SQLState
	return strings.HasPrefix(state, "08") || state == "40003" || state == "58004" || state == "58006" || state == "HYT00" || state == "HYT01"
}

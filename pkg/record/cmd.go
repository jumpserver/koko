package record

import (
	"time"
)

type Command struct {
	SessionID string
	StartTime time.Time
}

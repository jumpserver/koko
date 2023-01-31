package model

import (
	"time"
)

type ExpireInfo int64

func (e ExpireInfo) IsExpired(now time.Time) bool {
	return int64(e) < now.Unix()
}

package model

import (
	"time"
)

type ExpireInfo struct {
	HasPermission bool  `json:"has_permission"`
	ExpireAt      int64 `json:"expire_at"`

	Permission
}

func (e *ExpireInfo) IsExpired(now time.Time) bool {
	return e.ExpireAt < now.Unix()
}

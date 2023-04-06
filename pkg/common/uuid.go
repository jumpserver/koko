package common

import "github.com/google/uuid"

func UUID() string {
	return uuid.NewString()
}

func ValidUUIDString(sid string) bool {
	_, err := uuid.Parse(sid)
	return err == nil
}

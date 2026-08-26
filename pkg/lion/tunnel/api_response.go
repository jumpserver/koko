package tunnel

import (
	"errors"
)

var ErrNoAuthUser = errors.New("no auth user")

type APIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

func SuccessResponse(data interface{}) APIResponse {
	return APIResponse{
		Success: true,
		Data:    data,
	}
}

func ErrorResponse(err error) APIResponse {
	return APIResponse{
		Success: false,
		Message: err.Error(),
	}
}

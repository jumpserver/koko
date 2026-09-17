package auth

import (
	"errors"
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
)

const ContextKeyShareRecord = "validated-share-record"

// JoinShareRoom must only be called after the sharing WebSocket is established.
// The caller owns the returned record and must finish it when the connection ends.
func JoinShareRoom(ctx *gin.Context, client *service.JMService) (model.ShareRecord, error) {
	conf, err := client.GetTerminalConfig()
	if err != nil {
		return model.ShareRecord{}, err
	}
	if !conf.EnableSessionShare {
		return model.ShareRecord{}, errors.New("session sharing is disabled")
	}
	client = client.Copy()
	if lang := ctx.GetHeader("Accept-Language"); lang != "" {
		client.SetHeader("Accept-Language", lang)
	}
	if lang, err := ctx.Cookie("django_language"); err == nil {
		client.SetCookie("django_language", lang)
	}
	user := ctx.MustGet(ContextKeyUser).(*model.User)
	record, err := client.JoinShareRoom(model.SharePostData{
		ShareId: ctx.Query("target_id"), Code: ctx.Query("code"),
		UserId: user.ID, RemoteAddr: ctx.ClientIP(),
	})
	if record.Err != nil {
		return record, fmt.Errorf("%v", record.Err)
	}
	return record, err
}

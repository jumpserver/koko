package httpd

import "github.com/jumpserver/koko/pkg/model"

type WebContext struct {
	User       *model.User
	Connection *WebConn
	Client     *Client
}

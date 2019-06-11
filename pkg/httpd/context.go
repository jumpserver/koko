package httpd

import "koko/pkg/model"

type WebContext struct {
	User       *model.User
	Connection *WebConn
	Client     *Client
}

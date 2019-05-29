package httpd

import "cocogo/pkg/model"

type WebContext struct {
	User       *model.User
	Connection *WebConn
	Client     *Client
}

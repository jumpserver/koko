package auth

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/jumpserver/koko/pkg/logger"
)

const (
	authorizationHeader = "Authorization"
	dateHeader          = "Date"
	orgHeader           = "X-JMS-ORG"
)

func HTTPMiddleSessionAuth(jmsService *service.JMService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var (
			err  error
			user *model.User
		)
		reqCookies := ctx.Request.Cookies()
		var cookies = make(map[string]string)
		for _, cookie := range reqCookies {
			cookies[cookie.Name] = cookie.Value
		}
		if len(cookies) != 0 {
			user, err = jmsService.CheckUserCookie(cookies)
			if err == nil {
				ctx.Set(ContextKeyUser, user)
				return
			}

			logger.Errorf("Check user cookie failed: %s", err)
		}

		headers := requestAuthHeaders(ctx.Request)
		if len(headers) != 0 {
			user, err = jmsService.CheckUserHeaders(headers)
			if err == nil {
				ctx.Set(ContextKeyUser, user)
				return
			}

			logger.Errorf("Check user bearer failed: %s", err)
			ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"detail": "authentication failed",
			})
			return
		}

		ticketID := RequestConnectTicket(ctx.Request)
		if ticketID != "" {
			ticket, ok := ConnectTickets.Get(ticketID)
			if !ok || ticket.User == nil {
				ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"detail": "connect ticket invalid or expired",
				})
				return
			}

			if ticket.TokenID != "" {
				currentToken := strings.TrimSpace(ctx.Query("token"))
				if currentToken == "" || currentToken != ticket.TokenID {
					ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
						"detail": "connect ticket token mismatch",
					})
					return
				}
			}

			for key, value := range ticket.Headers {
				if strings.TrimSpace(value) == "" {
					continue
				}
				ctx.Request.Header.Set(key, value)
			}
			if strings.TrimSpace(ctx.Request.Header.Get(orgHeader)) == "" && ticket.OrgID != "" {
				ctx.Request.Header.Set(orgHeader, ticket.OrgID)
			}
			ctx.Set(ContextKeyUser, ticket.User)
			return
		}

		loginUrl := fmt.Sprintf("/core/auth/login/?next=%s", url.QueryEscape(ctx.Request.URL.RequestURI()))
		ctx.Redirect(http.StatusFound, loginUrl)
		ctx.Abort()
	}
}

func RequestAuthHeaders(req *http.Request) map[string]string {
	return requestAuthHeaders(req)
}

func requestAuthHeaders(req *http.Request) map[string]string {
	headers := map[string]string{}
	for _, key := range []string{authorizationHeader, dateHeader, orgHeader} {
		value := strings.TrimSpace(req.Header.Get(key))
		if value == "" {
			continue
		}
		headers[key] = value
	}
	return headers
}

func HTTPMiddleDebugAuth() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		switch ctx.ClientIP() {
		case "127.0.0.1", "localhost", "::1":
			return
		default:
			_ = ctx.AbortWithError(http.StatusBadRequest, fmt.Errorf("invalid host %s", ctx.ClientIP()))
			return
		}
	}
}

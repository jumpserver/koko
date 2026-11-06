package auth

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
)

const (
	authorizationHeader       = "Authorization"
	dateHeader                = "Date"
	orgHeader                 = "X-JMS-ORG"
	strictSessionHeader       = "X-JMS-Check-Session-Lock"
	ContextKeyHTTPAuthHeaders = "CONTEXT_HTTP_AUTH_HEADERS"
	ContextKeyConnectTicket   = "CONTEXT_CONNECT_TICKET"
)

func HTTPMiddleSessionAuth(jmsService *service.JMService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		headers := requestAuthHeaders(ctx.Request)
		// Explicit credentials must not inherit an unrelated browser session's lock state.
		// Once selected, a failed credential never falls back to another identity.
		if headers[authorizationHeader] != "" {
			checkUserProfile(ctx, jmsService, headers)
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

			if ticket.Headers[authorizationHeader] == "" && ticket.Headers["Cookie"] == "" {
				ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"detail": "connect ticket credentials missing",
				})
				return
			}
			// A ticket transports the original login; its cached user is not a new login.
			if !checkUserProfile(ctx, jmsService, ticket.Headers) {
				return
			}
			if ctx.MustGet(ContextKeyUser).(*model.User).ID != ticket.User.ID {
				ctx.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"detail": "connect ticket identity changed",
				})
				return
			}
			ctx.Set(ContextKeyConnectTicket, true)
			for _, key := range []string{authorizationHeader, dateHeader, orgHeader} {
				value := ticket.Headers[key]
				if strings.TrimSpace(value) == "" {
					continue
				}
				ctx.Request.Header.Set(key, value)
			}
			if strings.TrimSpace(ctx.Request.Header.Get(orgHeader)) == "" && ticket.OrgID != "" {
				ctx.Request.Header.Set(orgHeader, ticket.OrgID)
			}
			return
		}
		if cookies := ctx.Request.Header.Get("Cookie"); cookies != "" {
			headers["Cookie"] = cookies
			checkUserProfile(ctx, jmsService, headers)
			return
		}

		loginUrl := fmt.Sprintf("/core/auth/login/?next=%s", url.QueryEscape(ctx.Request.URL.RequestURI()))
		ctx.Redirect(http.StatusFound, loginUrl)
		ctx.Abort()
	}
}

func checkUserProfile(ctx *gin.Context, jmsService *service.JMService, headers map[string]string) bool {
	// CloneClient clears component credentials. Only Core receives these user credentials.
	client := jmsService.CloneClient()
	for _, key := range []string{authorizationHeader, dateHeader, orgHeader, "Cookie"} {
		if value := headers[key]; value != "" {
			client.SetHeader(key, value)
		}
	}
	client.SetHeader(strictSessionHeader, "1")
	var profile struct {
		model.User
		Code string `json:"code"`
	}
	response, err := client.Get(service.UserProfileURL, &profile)
	if err == nil && response != nil && response.StatusCode == http.StatusOK && profile.ID != "" {
		ctx.Set(ContextKeyUser, &profile.User)
		// Kept in memory for issuing connection tickets; never returned to the browser or logged.
		ctx.Set(ContextKeyHTTPAuthHeaders, cloneHeaders(headers))
		return true
	}
	status := http.StatusBadGateway
	if response != nil {
		switch response.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden, http.StatusLocked:
			status = response.StatusCode
		case http.StatusOK:
			status = http.StatusUnauthorized
		}
	}
	body := gin.H{"detail": "authentication failed"}
	if profile.Code == "web_session_locked" || profile.Code == "client_locked" {
		body = gin.H{"code": profile.Code, "locked": true}
	}
	ctx.AbortWithStatusJSON(status, body)
	return false
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

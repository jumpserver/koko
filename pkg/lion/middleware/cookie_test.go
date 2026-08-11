package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/jumpserver/koko/pkg/auth"
	"github.com/jumpserver/koko/pkg/config"
)

func TestJmsCookieAuthAcceptsBoundConnectTicket(t *testing.T) {
	core := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(core.Close)

	jmsService, err := service.NewAuthJMService(service.JMSCoreHost(core.URL))
	if err != nil {
		t.Fatalf("create JMService: %s", err)
	}
	ticket := auth.ConnectTickets.Create(&model.User{ID: "user-1"}, nil, "token-1", "")

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/lion/test", JmsCookieAuth(jmsService), func(ctx *gin.Context) {
		userValue, ok := ctx.Get(config.GinCtxUserKey)
		if !ok || userValue.(*model.User).ID != "user-1" {
			ctx.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		ctx.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet,
		"/lion/test?ticket="+ticket.ID+"&token=token-1", nil)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("valid connect ticket status = %d, want %d", response.Code, http.StatusNoContent)
	}

	mismatchRequest := httptest.NewRequest(http.MethodGet,
		"/lion/test?ticket="+ticket.ID+"&token=other-token", nil)
	mismatchResponse := httptest.NewRecorder()
	engine.ServeHTTP(mismatchResponse, mismatchRequest)
	if mismatchResponse.Code != http.StatusUnauthorized {
		t.Fatalf("mismatched connect ticket status = %d, want %d", mismatchResponse.Code, http.StatusUnauthorized)
	}
}

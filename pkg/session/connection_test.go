package session

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
)

func TestBindConnectionRequiresMonitoringButToleratesTicketFailure(t *testing.T) {
	var monitorStatus atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+service.TicketSessionURL, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "ticket unavailable", http.StatusServiceUnavailable)
	})
	mux.HandleFunc("POST "+service.FaceMonitorContextUrl, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(int(monitorStatus.Load()))
		_, _ = w.Write([]byte(`{}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	client, err := service.NewAuthJMService(service.JMSCoreHost(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	token := &model.ConnectToken{Ticket: &model.ObjectId{ID: "ticket"}, FaceMonitorToken: "monitor"}
	for _, status := range []int{http.StatusOK, http.StatusForbidden} {
		monitorStatus.Store(int32(status))
		reported := false
		err := BindConnection(client, "session", token, func(error) { reported = true })
		if !reported || (err != nil) != (status != http.StatusOK) {
			t.Fatalf("monitor status %d: ticket reported=%v, error=%v", status, reported, err)
		}
	}
}

func TestPermissionGrace(t *testing.T) {
	var grace PermissionGrace
	start := time.Now()
	grace.Revoke(start)
	grace.Revoke(start.Add(5 * time.Minute))
	if grace.Expired(start.Add(9*time.Minute)) || !grace.Expired(start.Add(10*time.Minute)) {
		t.Fatal("repeated revocation changed the ten-minute grace period")
	}
	grace.Restore()
	if grace.Revoked() || grace.Expired(start.Add(time.Hour)) {
		t.Fatal("restored permission still expires")
	}
}

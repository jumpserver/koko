package session

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service/panda"
)

func TestPandaClientContainerProtocol(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Authorization") == "" || r.Header.Get("X-JMS-ORG") != "ROOT" {
			t.Errorf("missing authenticated Panda request")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case panda.ContainerCreateURL:
			var body struct {
				Token string                 `json:"token"`
				App   model.VirtualAppOption `json:"virtual_app"`
			}
			decoder := json.NewDecoder(r.Body)
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&body); err != nil {
				t.Error(err)
			}
			if body.Token != "token" || body.App.ImageName != "browser:v1" || body.App.DesktopWidth != 1280 {
				t.Errorf("lost virtual app request fields")
			}
			_ = json.NewEncoder(w).Encode(panda.Response{Success: true, Data: model.VirtualAppContainer{
				ContainerId: "container", Host: "127.0.0.1", Port: 6900, SFTPPort: 6901,
			}})
		case panda.ContainerReleaseURL:
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["container_id"] != "container" {
				t.Errorf("invalid container release request")
			}
			_, _ = w.Write([]byte(`{"success":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewPandaClient(server.URL, model.AccessKey{ID: "key", Secret: "secret"}, false)
	container, err := client.CreateContainer("token", model.VirtualAppOption{
		ImageName: "browser:v1", DesktopWidth: 1280,
	})
	if err != nil {
		t.Fatal(err)
	}
	if container.Host != "127.0.0.1" || container.Port != 6900 || container.SFTPPort != 6901 {
		t.Fatalf("lost container connection fields: %+v", container)
	}
	if err := client.ReleaseContainer(container.ContainerId); err != nil {
		t.Fatal(err)
	}
}

func TestPandaClientCreateFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":false,"message":"container failed"}`))
	}))
	defer server.Close()
	client := NewPandaClient(server.URL, model.AccessKey{ID: "key", Secret: "secret"}, false)
	if _, err := client.CreateContainer("token", model.VirtualAppOption{}); err == nil {
		t.Fatal("Panda failure was accepted as a created container")
	}
}

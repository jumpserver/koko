package common

import (
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/jarcoal/httpmock"
)

const (
	username = "admin"
	password = "admin"
	baseHost = "http://localhost"
	usersUrl = "/api/v1/users"
)

type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Age  int    `json:"age"`
}

var users = []User{{ID: 1, Name: "Guanghongwei", Age: 18}, {ID: 2, Name: "ibuler", Age: 19}}
var user = User{ID: 2, Name: "Jumpserver", Age: 5}
var userDeleteUrl = fmt.Sprintf("%s/%d", usersUrl, user.ID)

func TestClient_Do(t *testing.T) {
	c := NewClient(10, "")
	_, err := c.Do("GET", usersUrl, nil, nil)
	if err == nil {
		t.Error("Failed Do(), want get err but not")
	}
	c.SetBasicAuth(username, password)
	var res []User
	_, err = c.Do("GET", usersUrl, nil, &res)
	if err != nil {
		t.Errorf("Failed Do() error: %s", err.Error())
	}
	if len(res) != 2 {
		t.Errorf("user not equal 2: %d", len(res))
	}
}

func TestClient_Get(t *testing.T) {
	c := NewClient(10, baseHost)
	_, err := c.Get(usersUrl, nil)
	if err == nil {
		t.Errorf("Failed Get(%s): want get err but not", usersUrl)
	}
	c.SetBasicAuth(username, password)
	_, err = c.Get(usersUrl, nil)
	if err != nil {
		t.Errorf("Failed Get(%s): %s", usersUrl, err.Error())
	}
}

func TestClient_Post(t *testing.T) {
	c := NewClient(10, baseHost)
	var userCreated User
	_, err := c.Post(usersUrl, user, &userCreated)
	if err != nil {
		t.Errorf("Failed Post(): %s", err.Error())
	}
	if userCreated.ID != user.ID {
		t.Errorf("Failed Post(), id not euqal: %d != %d", userCreated.ID, user.ID)
	}
}

func TestClient_Put(t *testing.T) {
	c := NewClient(10, "")
	var userUpdated User
	_, err := c.Put(usersUrl, user, &userUpdated)
	if err != nil {
		t.Errorf("Failed Put(): %s", err.Error())
	}
	if userUpdated.ID != user.ID {
		t.Errorf("Failed Post(), id not euqal: %d != %d", userUpdated.ID, user.ID)
	}
}

func TestClient_Delete(t *testing.T) {
	c := NewClient(10, baseHost)
	c.SetBasicAuth(username, password)
	_, err := c.Delete(userDeleteUrl, nil)
	if err != nil {
		t.Errorf("Failed Delete(): %s", err.Error())
	}
}

func PrepareMockData() {
	httpmock.RegisterResponder("GET", usersUrl,
		func(req *http.Request) (*http.Response, error) {
			u, p, ok := req.BasicAuth()
			if !ok || u != username || p != password {
				return httpmock.NewStringResponse(401, ""), nil
			}
			resp, err := httpmock.NewJsonResponse(200, users)
			if err != nil {
				return httpmock.NewStringResponse(500, ""), nil
			}
			return resp, nil
		},
	)

	resp, err := httpmock.NewJsonResponder(201, user)
	if err != nil {
		fmt.Println("Create post reps failed")
	}
	httpmock.RegisterResponder("POST", usersUrl, resp)
	httpmock.RegisterResponder("PUT", usersUrl, resp)
	httpmock.RegisterResponder("DELETE", userDeleteUrl, httpmock.NewStringResponder(204, ""))
}

func TestMain(m *testing.M) {
	httpmock.Activate()
	PrepareMockData()
	defer httpmock.DeactivateAndReset()
	code := m.Run()
	os.Exit(code)
}

func TestConvertSizeToBytes(t *testing.T) {
	data := map[string]int{
		"100M": 100 * 1024 * 1024,
		"10M":  10 * 1024 * 1024,
		"1G":  1024 * 1024 * 1024,
		"1024":1024,
	}
	for k, v := range data {
		convValue := ConvertSizeToBytes(k)
		if convValue != v {
			t.Errorf("%s should be equale to %d bytes, but conver to %d", k, v, convValue)
		}
		t.Logf("%s convert to %d bytes", k, convValue)
	}
}

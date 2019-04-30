package common

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	neturl "net/url"
	"reflect"
	"strings"
	"time"
)

type ClientAuth interface {
	Sign() (date, sign string)
}

type Client struct {
	Timeout    time.Duration
	Headers    map[string]string
	Auth       ClientAuth
	basicAuth  []string
	cookie     map[string]string
	http       *http.Client
	UrlParsers []UrlParser
}

type UrlParser interface {
	parse(url string, params ...map[string]string) string
}

func NewClient(timeout time.Duration) *Client {
	headers := make(map[string]string, 1)
	client := http.DefaultClient
	client.Timeout = timeout * time.Second
	return &Client{
		Timeout: timeout * time.Second,
		Headers: headers,
		http:    client,
		cookie:  make(map[string]string, 0),
	}
}

func (c *Client) SetCookie(k, v string) {
	c.cookie[k] = v
}

func (c *Client) SetBasicAuth(username, password string) {
	c.basicAuth = append(c.basicAuth, username)
	c.basicAuth = append(c.basicAuth, password)
}

func (c *Client) SetAuth(auth ClientAuth) {
	c.Auth = auth
}

func (c *Client) marshalData(data interface{}) (reader io.Reader, error error) {
	dataRaw, err := json.Marshal(data)
	if err != nil {
		return
	}
	reader = bytes.NewReader(dataRaw)
	return
}

func (c *Client) ParseUrlQuery(url string, query map[string]string) string {
	var paramSlice []string
	for k, v := range query {
		paramSlice = append(paramSlice, fmt.Sprintf("%s=%s", k, v))
	}
	param := strings.Join(paramSlice, "&")
	if strings.Contains(url, "?") {
		url += "&" + param
	} else {
		url += "?" + param
	}
	return url
}

func (c *Client) ParseUrl(url string) string {
	return url
}

func (c *Client) SetAuthHeader(r *http.Request, params ...map[string]string) {
	if len(c.cookie) != 0 {
		cookie := make([]string, 0)
		for k, v := range c.cookie {
			cookie = append(cookie, fmt.Sprintf("%s=%s", k, v))
		}
		r.Header.Add("Cookie", strings.Join(cookie, ";"))
	}
	if len(c.basicAuth) == 2 {
		r.SetBasicAuth(c.basicAuth[0], c.basicAuth[1])
		return
	}
	if c.Auth != nil {
		date, sign := c.Auth.Sign()
		r.Header.Set("Date", date)
		r.Header.Set("Authorization", sign)
	}
}

func (c *Client) SetReqHeaders(req *http.Request, params ...map[string]string) {
	if len(c.Headers) != 0 {
		for k, v := range c.Headers {
			req.Header.Set(k, v)
		}
	}
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", "coco-client")
	c.SetAuthHeader(req)
}

func (c *Client) NewRequest(method, url string, body interface{}) (req *http.Request, err error) {
	url = c.ParseUrl(url)
	reader, err := c.marshalData(body)
	if err != nil {
		return
	}
	req, err = http.NewRequest(method, url, reader)
	c.SetReqHeaders(req)
	return req, err
}

// Do wrapper http.Client Do() for using auth and error handle
// params:
//   1. query string if set {"name": "ibuler"}
func (c *Client) Do(method, url string, data, res interface{}, params ...map[string]string) (err error) {
	req, err := c.NewRequest(method, url, data)
	resp, err := c.http.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		msg := fmt.Sprintf("%s %s failed, get code: %d, %s", req.Method, req.URL, resp.StatusCode, string(body))
		err = errors.New(msg)
		return
	}

	// Unmarshal response body to result struct
	if res != nil {
		err = json.Unmarshal(body, res)
		if err != nil {
			msg := fmt.Sprintf("%s %s failed, unmarshal `%s` response failed", req.Method, req.URL, string(body)[:50])
			err = errors.New(msg)
			return
		}
	}
	return
}

func (c *Client) Get(url string, res interface{}) (err error) {
	return c.Do("GET", url, nil, res)
}

func (c *Client) Post(url string, data interface{}, res interface{}) (err error) {
	return c.Do("POST", url, data, res)
}

func (c *Client) Delete(url string, res interface{}) (err error) {
	return c.Do("DELETE", url, nil, res)
}

func (c *Client) Put(url string, data interface{}, res interface{}) (err error) {
	return c.Do("PUT", url, data, res)
}

func (c *Client) Patch(url string, data interface{}, res interface{}) (err error) {
	return c.Do("PATCH", url, data, res)
}

func (c *Client) PostForm(url string, data interface{}, res interface{}) (err error) {
	values := neturl.Values{}
	if data != nil {
		rcvr := reflect.ValueOf(data)
		tp := reflect.Indirect(rcvr).Type()
		val := reflect.Indirect(rcvr)

		for i := 0; i < tp.NumField(); i++ {
			tag := tp.Field(i).Tag.Get("json")
			var v string
			switch tp.Field(i).Type.Name() {
			case "string":
				v = val.Field(i).String()
			default:
				attr, err := json.Marshal(val.Field(i).Interface())
				if err != nil {
					return nil
				}
				v = string(attr)
			}
			values.Set(tag, v)
		}
	}

	reader := strings.NewReader(values.Encode())
	req, err := http.NewRequest("POST", url, reader)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return nil
}

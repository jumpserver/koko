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

type Client struct {
	Timeout       time.Duration
	Headers       map[string]string
	BaseHost      string
	basicAuth     []string
	authorization string
	cookie        map[string]string
}

func NewClient() *Client {
	headers := make(map[string]string, 1)
	return &Client{
		Timeout: 30,
		Headers: headers,
		cookie:  make(map[string]string, 0),
	}
}

func (c *Client) SetBasicAuth(username, password string) {
	c.basicAuth = append(c.basicAuth, username)
	c.basicAuth = append(c.basicAuth, password)
}

func (c *Client) SetAuth(authorization string) {
	c.authorization = authorization
}

func (c *Client) SetCookie(k, v string) {
	c.cookie[k] = v
}

func (c *Client) marshalData(data interface{}) (reader io.Reader, error error) {
	dataRaw, err := json.Marshal(data)
	if err != nil {
		return
	}
	reader = bytes.NewReader(dataRaw)
	return
}

func (c *Client) ParseUrl(url string) string {
	return url
}

func (c *Client) ConstructUrl(url string) string {
	if c.BaseHost != "" {
		url = strings.TrimRight(c.BaseHost, "/") + url
	}
	return url
}

func (c *Client) NewRequest(method, url string, body interface{}) (req *http.Request, err error) {
	url = c.ConstructUrl(url)
	reader, err := c.marshalData(body)
	if err != nil {
		return
	}
	return http.NewRequest(method, url, reader)
}

// Do wrapper http.Client Do() for using auth and error handle
func (c *Client) Do(req *http.Request, res interface{}) (err error) {
	// Custom our client
	client := http.DefaultClient
	client.Timeout = c.Timeout * time.Second
	if len(c.basicAuth) == 2 {
		req.SetBasicAuth(c.basicAuth[0], c.basicAuth[1])
	}
	if c.authorization != "" {
		req.Header.Add("Authorization", c.authorization)
	}
	if len(c.cookie) != 0 {
		cookie := make([]string, 0)
		for k, v := range c.cookie {
			cookie = append(cookie, fmt.Sprintf("%s=%s", k, v))
		}
		req.Header.Add("Cookie", strings.Join(cookie, ";"))
	}
	if len(c.Headers) != 0 {
		for k, v := range c.Headers {
			req.Header.Set(k, v)
		}
	}
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", "coco-client")

	// Request it
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		err = errors.New(fmt.Sprintf("%s %s failed, get code: %d, %s", req.Method, req.URL, resp.StatusCode, string(body)))
		return
	}

	// Unmarshal response body to result struct
	if res != nil {
		err = json.Unmarshal(body, res)
		if err != nil {
			msg := fmt.Sprintf("Failed %s %s, unmarshal `%s` response failed", req.Method, req.URL, string(body)[:50])
			return errors.New(msg)
		}
	}
	return nil
}

func (c *Client) Get(url string, res interface{}, params ...map[string]string) (err error) {
	if len(params) == 1 {
		paramSlice := make([]string, 1)
		for k, v := range params[0] {
			paramSlice = append(paramSlice, fmt.Sprintf("%s=%s", k, v))
		}
		param := strings.Join(paramSlice, "&")
		if strings.Contains(url, "?") {
			url += "&" + param
		} else {
			url += "?" + param
		}
	}
	req, err := c.NewRequest("GET", url, nil)
	if err != nil {
		return
	}
	err = c.Do(req, res)
	return err
}

func (c *Client) Post(url string, data interface{}, res interface{}) (err error) {
	req, err := c.NewRequest("POST", url, data)
	if err != nil {
		return
	}
	err = c.Do(req, res)
	return err
}

func (c *Client) Delete(url string, res interface{}) (err error) {
	req, err := c.NewRequest("DELETE", url, nil)
	err = c.Do(req, res)
	return err
}

func (c *Client) Put(url string, data interface{}, res interface{}) (err error) {
	req, err := c.NewRequest("PUT", url, data)
	if err != nil {
		return
	}
	err = c.Do(req, res)
	return err
}

func (c *Client) Patch(url string, data interface{}, res interface{}) (err error) {
	req, err := c.NewRequest("PATCH", url, data)
	if err != nil {
		return
	}
	err = c.Do(req, res)
	return err
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
					return err
				}
				v = string(attr)
			}
			values.Set(tag, v)
		}
	}

	reader := strings.NewReader(values.Encode())
	req, err := http.NewRequest("POST", url, reader)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	err = c.Do(req, res)
	return err
}

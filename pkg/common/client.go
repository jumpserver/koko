package common

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	neturl "net/url"
	"os"
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
	BaseHost   string
	basicAuth  []string
	cookie     map[string]string
	http       http.Client
	UrlParsers []UrlParser
}

type UrlParser interface {
	parse(url string, params ...map[string]string) string
}

func NewClient(timeout time.Duration, baseHost string) Client {
	headers := make(map[string]string)
	jar := &customCookieJar{
		data: map[string]string{},
	}
	client := http.Client{
		Timeout: timeout * time.Second,
		Jar:     jar,
	}
	return Client{
		BaseHost: baseHost,
		Timeout:  timeout * time.Second,
		Headers:  headers,
		http:     client,
		cookie:   make(map[string]string),
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

func (c *Client) SetHeader(k, v string) {
	c.Headers[k] = v
}

func (c *Client) marshalData(data interface{}) (reader io.Reader, error error) {
	dataRaw, err := json.Marshal(data)
	if err != nil {
		return
	}
	reader = bytes.NewReader(dataRaw)
	return
}

func (c *Client) parseUrlQuery(url string, params []map[string]string) string {
	if len(params) < 1 {
		return url
	}
	query := neturl.Values{}
	for _, item := range params {
		for k, v := range item {
			query.Add(k, v)
		}
	}
	if strings.Contains(url, "?") {
		url += "&" + query.Encode()
	} else {
		url += "?" + query.Encode()
	}
	return url
}

func (c *Client) parseUrl(url string, params []map[string]string) string {
	url = c.parseUrlQuery(url, params)
	if c.BaseHost != "" {
		url = strings.TrimRight(c.BaseHost, "/") + url
	}
	return url
}

func (c *Client) setAuthHeader(r *http.Request) {
	if len(c.cookie) != 0 {
		for k, v := range c.cookie {
			c := http.Cookie{Name: k, Value: v}
			r.AddCookie(&c)
		}
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

func (c *Client) SetReqHeaders(req *http.Request, params []map[string]string) {
	if len(c.Headers) != 0 {
		for k, v := range c.Headers {
			req.Header.Set(k, v)
		}
	}
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", "koko-client")
	c.setAuthHeader(req)
	if len(params) >= 2 {
		for k, v := range params[1] {
			req.Header.Set(k, v)
		}
	}
}

func (c *Client) NewRequest(method, url string, body interface{}, params []map[string]string) (req *http.Request, err error) {
	url = c.parseUrl(url, params)
	reader, err := c.marshalData(body)
	if err != nil {
		return
	}
	req, err = http.NewRequest(method, url, reader)
	if err != nil {
		return req, err
	}
	c.SetReqHeaders(req, params)
	return req, err
}

// Do wrapper http.Client Do() for using auth and error handle
// params:
//   1. query string if set {"name": "ibuler"}
func (c *Client) Do(method, url string, data, res interface{}, params ...map[string]string) (resp *http.Response, err error) {
	req, err := c.NewRequest(method, url, data, params)
	if err != nil {
		return
	}
	resp, err = c.http.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		msg := fmt.Sprintf("%s %s failed, get code: %d, %s", req.Method, req.URL, resp.StatusCode, body)
		err = errors.New(msg)
		return
	}

	// If is buffer return the raw response body
	if buf, ok := res.(*bytes.Buffer); ok {
		buf.Write(body)
		return
	}
	// Unmarshal response body to result struct
	if res != nil {
		switch {
		case strings.Contains(resp.Header.Get("Content-Type"), "application/json"):
			err = json.Unmarshal(body, res)
			if err != nil {
				msg := fmt.Sprintf("%s %s failed, unmarshal '%s' response failed: %s", req.Method, req.URL, body[:12], err)
				err = errors.New(msg)
				return
			}
		}
	}
	return
}

func (c *Client) Get(url string, res interface{}, params ...map[string]string) (resp *http.Response, err error) {
	return c.Do("GET", url, nil, res, params...)
}

func (c *Client) Post(url string, data interface{}, res interface{}, params ...map[string]string) (resp *http.Response, err error) {
	return c.Do("POST", url, data, res, params...)
}

func (c *Client) Delete(url string, res interface{}, params ...map[string]string) (resp *http.Response, err error) {
	return c.Do("DELETE", url, nil, res, params...)
}

func (c *Client) Put(url string, data interface{}, res interface{}, params ...map[string]string) (resp *http.Response, err error) {
	return c.Do("PUT", url, data, res, params...)
}

func (c *Client) Patch(url string, data interface{}, res interface{}, params ...map[string]string) (resp *http.Response, err error) {
	return c.Do("PATCH", url, data, res, params...)
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
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return nil
}
func (c *Client) UploadFile(reqUrl string, gFile string, res interface{}, params ...map[string]string) (err error) {
	fd, err := os.Open(gFile)
	if err != nil {
		return err
	}
	// 缓存读取内容，减少系统调用
	bufferFd := bufio.NewReader(fd)
	defer fd.Close()
	fi, err := fd.Stat()
	if err != nil {
		return err
	}
	contentType, contentLen, bodyReader := getFileMultipartBodyReader("file", fd.Name(), fi.Size(), bufferFd)
	reqUrl = c.parseUrl(reqUrl, params)
	req, err := http.NewRequest(http.MethodPost, reqUrl, bodyReader)
	if err != nil {
		return err
	}
	req.ContentLength = contentLen
	req.Header.Set("Content-Type", contentType)
	c.SetReqHeaders(req, params)
	/*
		上传文件时，取消 timeout
		A Timeout of zero means no timeout.
	*/
	client := http.Client{
		Jar: c.http.Jar,
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		msg := fmt.Sprintf("%s %s failed, get code: %d, %s", req.Method, req.URL, resp.StatusCode, string(body))
		err = errors.New(msg)
		return
	}

	// If is buffer return the raw response body
	if buf, ok := res.(*bytes.Buffer); ok {
		buf.Write(body)
		return
	}
	// Unmarshal response body to result struct
	if res != nil {
		err = json.Unmarshal(body, res)
		if err != nil {
			msg := fmt.Sprintf("%s %s failed, unmarshal '%s' response failed: %s", req.Method, req.URL, body, err)
			err = errors.New(msg)
			return
		}
	}
	return
}

func getFileMultipartBodyReader(field, filename string, size int64,
	reader io.Reader) (contentType string, contentLen int64, bodyReader io.Reader) {
	startPartBuf := bytes.NewBufferString("")
	bodyWriter := multipart.NewWriter(startPartBuf)
	// use the bodyWriter to write the Part headers to the buffer
	_, _ = bodyWriter.CreateFormFile(field, filename)
	boundary := bodyWriter.Boundary()
	endString := fmt.Sprintf("\r\n--%s--\r\n", boundary)
	endPartBuf := bytes.NewBufferString(endString)

	bodyReader = io.MultiReader(startPartBuf, reader, endPartBuf)
	contentLen = int64(startPartBuf.Len()) + size + int64(endPartBuf.Len())
	contentType = bodyWriter.FormDataContentType()
	return
}

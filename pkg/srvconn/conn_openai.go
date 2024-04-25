package srvconn

import (
	"context"
	"crypto/tls"
	"errors"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/sashabaranov/go-openai"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type TransportOptions struct {
	UseProxy        bool
	ProxyURL        *url.URL
	SkipCertificate bool
}

type TransportOption func(*TransportOptions)

func WithProxy(proxyURL string) TransportOption {
	UseProxy := proxyURL != ""
	proxy, err := url.Parse(proxyURL)
	if err != nil {
		proxy = nil
		UseProxy = false
		logger.Errorf("Proxy URL parse error: %s", err.Error())
	}
	return func(opts *TransportOptions) {
		opts.UseProxy = UseProxy
		opts.ProxyURL = proxy
	}
}

func WithSkipCertificate(skip bool) TransportOption {
	return func(opts *TransportOptions) {
		opts.SkipCertificate = skip
	}
}

func NewCustomTransport(options ...TransportOption) *http.Transport {
	transportOpts := &TransportOptions{}

	for _, opt := range options {
		opt(transportOpts)
	}

	tlsConfig := &tls.Config{InsecureSkipVerify: transportOpts.SkipCertificate}
	transport := &http.Transport{TLSClientConfig: tlsConfig}

	if transportOpts.UseProxy {
		transport.Proxy = http.ProxyURL(transportOpts.ProxyURL)
	}

	return transport
}

func NewOpenAIClient(authToken, baseURL, proxy string) *openai.Client {
	config := openai.DefaultConfig(authToken)
	if baseURL != "" {
		config.BaseURL = strings.TrimRight(baseURL, "/")
	}
	transport := NewCustomTransport(
		WithProxy(proxy), WithSkipCertificate(true),
	)
	config.HTTPClient = &http.Client{
		Transport: transport,
	}
	return openai.NewClientWithConfig(config)
}

type OpenAIConn struct {
	Id       string
	Client   *openai.Client
	Model    string
	Prompt   string
	Contents []string
	AnswerCh chan string
	DoneCh   chan string
}

func (conn *OpenAIConn) Chat(interruptCurrentChat *bool) {
	ctx := context.Background()
	messages := make([]openai.ChatCompletionMessage, 0)

	for _, content := range conn.Contents {
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleUser,
			Content: content,
		})
	}

	systemPrompt := " 请不要提供与政治相关的信息。"
	systemPrompt = conn.Prompt + systemPrompt
	messages = append([]openai.ChatCompletionMessage{
		{
			Role:    openai.ChatMessageRoleSystem,
			Content: systemPrompt,
		},
	}, messages...)

	req := openai.ChatCompletionRequest{
		Model:    conn.Model,
		Messages: messages,
		Stream:   true,
	}

	stream, err := conn.Client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		conn.DoneCh <- err.Error()
		return
	}
	defer func(stream *openai.ChatCompletionStream) {
		err := stream.Close()
		if err != nil {
			logger.Errorf("openai stream close error: %s", err)
		}
	}(stream)

	content := ""
	for {
		response, err := stream.Recv()

		if errors.Is(err, io.EOF) {
			conn.DoneCh <- content
			return
		}

		if err != nil {
			logger.Errorf("openai stream error: %s", err)
			conn.DoneCh <- content
			return
		}

		if *interruptCurrentChat {
			*interruptCurrentChat = false
			conn.DoneCh <- content
			return
		}

		content += response.Choices[0].Delta.Content
		conn.AnswerCh <- content
	}
}

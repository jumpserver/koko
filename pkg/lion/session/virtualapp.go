package session

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/jumpserver-dev/sdk-go/service/panda"
	"github.com/jumpserver/koko/pkg/lion/gateway"
)

// Keep provider routing compatible with the published SDK and legacy Core.
type virtualApp struct {
	model.VirtualApp
	Provider *virtualAppProvider `json:"provider"`
}

type virtualAppProvider struct {
	Name           string         `json:"name"`
	ConnectionMode string         `json:"connection_mode"`
	ServiceURL     string         `json:"service_url"`
	Host           model.Asset    `json:"host"`
	Account        model.Account  `json:"account"`
	Gateway        *model.Gateway `json:"gateway"`
}

func (s *Server) getVirtualAppOption(token string) (app virtualApp, err error) {
	_, err = s.JmsService.Call(http.MethodPost, service.SuperConnectTokenVirtualAppOptionURL,
		map[string]string{"id": token}, &app)
	return
}

func (s *Server) pandaClientFor(provider *virtualAppProvider) *panda.Client {
	if provider != nil && provider.ServiceURL != "" {
		if s.PandaClientFactory == nil {
			return nil
		}
		return s.PandaClientFactory(provider.ServiceURL)
	}
	return s.PandaClient
}

func providerSSHTarget(provider *virtualAppProvider) *model.Gateway {
	return &model.Gateway{
		ID:        provider.Host.ID,
		Name:      provider.Name,
		Address:   provider.Host.Address,
		Protocols: provider.Host.Protocols,
		Account:   provider.Account,
	}
}

func pandaAPIForwardAddress(serviceURL string) (string, *url.URL, error) {
	parsed, err := url.Parse(serviceURL)
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return "", nil, fmt.Errorf("invalid Panda service URL %q", serviceURL)
	}
	port := parsed.Port()
	if port == "" {
		if parsed.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return net.JoinHostPort("127.0.0.1", port), parsed, nil
}

func (s *Server) startPandaAPIForward(ctx context.Context, provider *virtualAppProvider) (*gateway.DomainGateway, string, error) {
	dstAddr, parsedURL, err := pandaAPIForwardAddress(provider.ServiceURL)
	if err != nil {
		return nil, "", err
	}
	forwarder := &gateway.DomainGateway{
		DstAddr:         dstAddr,
		SelectedGateway: provider.Gateway,
		Destination:     providerSSHTarget(provider),
	}
	// Cancellation must interrupt setup, but the API tunnel must survive a
	// disconnected browser until ReleaseContainer has finished.
	stop := context.AfterFunc(ctx, forwarder.Stop)
	err = forwarder.StartContext(context.WithoutCancel(ctx))
	if !stop() || ctx.Err() != nil {
		forwarder.Stop()
		return nil, "", ctx.Err()
	}
	if err != nil {
		return nil, "", err
	}
	parsedURL.Host = forwarder.GetListenAddr().String()
	return forwarder, parsedURL.String(), nil
}

package session

import (
	"context"
	"errors"
	"net/http"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/jumpserver/koko/pkg/lion/gateway"
)

// Include provider SSH routing alongside the published SDK virtual app fields.
type virtualApp struct {
	model.VirtualApp
	Provider *virtualAppProvider `json:"provider"`
}

type virtualAppProvider struct {
	Name    string         `json:"name"`
	Host    model.Asset    `json:"host"`
	Account model.Account  `json:"account"`
	Gateway *model.Gateway `json:"gateway"`
}

func (s *Server) getVirtualAppOption(token string) (app virtualApp, err error) {
	_, err = s.JmsService.Call(http.MethodPost, service.SuperConnectTokenVirtualAppOptionURL,
		map[string]string{"id": token}, &app)
	return
}

func providerSSHTarget(provider *virtualAppProvider) (*model.Gateway, error) {
	if provider == nil {
		return nil, errors.New("virtual app provider is required")
	}
	if provider.Host.Address == "" {
		return nil, errors.New("virtual app provider SSH host is required")
	}
	if port := model.Protocols(provider.Host.Protocols).GetProtocolPort("ssh"); port < 1 || port > 65535 {
		return nil, errors.New("virtual app provider requires a valid SSH port")
	}
	if provider.Account.Username == "" || provider.Account.Secret == "" {
		return nil, errors.New("virtual app provider SSH account and credentials are required")
	}
	return &model.Gateway{
		ID:        provider.Host.ID,
		Name:      provider.Name,
		Address:   provider.Host.Address,
		Protocols: provider.Host.Protocols,
		Account:   provider.Account,
	}, nil
}

func (s *Server) startPandaAPIForward(ctx context.Context, provider *virtualAppProvider) (*gateway.DomainGateway, string, error) {
	target, err := providerSSHTarget(provider)
	if err != nil {
		return nil, "", err
	}
	forwarder := &gateway.DomainGateway{
		DstAddr:         "127.0.0.1:9001",
		SelectedGateway: provider.Gateway,
		Destination:     target,
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
	return forwarder, "http://" + forwarder.GetListenAddr().String(), nil
}

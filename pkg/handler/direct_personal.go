package handler

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/gliderlabs/ssh"
	"github.com/jumpserver-dev/sdk-go/common"
	"github.com/jumpserver-dev/sdk-go/httplib"
	"github.com/jumpserver-dev/sdk-go/model"

	"github.com/jumpserver/koko/pkg/auth"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/sshcert"
)

func (s *Server) prepareDirectRequest(sess ssh.Session, user *model.User,
	req *auth.DirectLoginAssetReq, interactive bool) (*auth.DirectLoginAssetReq, *WrapperSession, error) {
	var terminal io.ReadWriteCloser
	var wrapper *WrapperSession
	if interactive && !req.IsToken() && common.IsUUID(req.AccountUsername) {
		wrapper = NewWrapperSession(sess)
		terminal = wrapper
	}
	token, err := s.directPersonalToken(sess.Context(), user, req, terminal)
	if err != nil || token == nil {
		return req, wrapper, err
	}
	// The login request belongs to the SSH connection, which can open several channels.
	prepared := *req
	prepared.ConnectToken = token
	return &prepared, wrapper, nil
}

func (s *Server) directPersonalToken(ctx ssh.Context, user *model.User,
	req *auth.DirectLoginAssetReq, terminal io.ReadWriteCloser) (*model.ConnectToken, error) {
	if req.IsToken() || !common.IsUUID(req.AccountUsername) {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	assets, err := s.getMatchedAssetsByDirectReq(user, req)
	if err != nil {
		return nil, err
	}
	if len(assets) != 1 {
		for _, asset := range assets {
			detail, err := s.jmsService.GetUserPermAssetDetailById(user.ID, asset.ID)
			if err != nil {
				// Let the existing asset picker handle failures for unrelated assets.
				return nil, nil
			}
			if len(GetMatchedAccounts(detail.PermedAccounts, req.AccountUsername)) > 0 {
				return nil, nil
			}
		}
		return nil, fmt.Errorf("personal credential direct login requires a unique asset for %s", req.AssetTarget)
	}
	asset := assets[0]
	detail, err := s.jmsService.GetUserPermAssetDetailById(user.ID, asset.ID)
	if err != nil {
		return nil, err
	}
	// Preserve existing direct login for accounts whose username is itself a UUID.
	if len(GetMatchedAccounts(detail.PermedAccounts, req.AccountUsername)) > 0 {
		return nil, nil
	}
	login, ok := ctx.Value(auth.ContextKeyClient).(*auth.UserAuthClient)
	if !ok || login == nil || login.BearerToken() == "" {
		return nil, errors.New("personal credential authentication is unavailable; please reconnect to Koko")
	}
	if detail.ID != asset.ID || detail.OrgID != "" && asset.OrgID != "" && detail.OrgID != asset.OrgID {
		return nil, errors.New("asset details do not match the selected asset")
	}
	if detail.OrgID != "" {
		asset.OrgID = detail.OrgID
	}
	client := s.jmsService.CloneClient()
	client.SetAuthSign(&httplib.BearerTokenAuth{Token: login.BearerToken()})
	client.SetHeader("X-JMS-ORG", asset.OrgID)
	locale := getUserDefaultLangCode(user)
	client.SetHeader("Accept-Language", i18n.NewLang(locale).CoreCode())
	remoteAddr, _, _ := net.SplitHostPort(ctx.RemoteAddr().String())
	client.SetHeader("X-Forwarded-For", remoteAddr)
	var credential struct {
		ID       string           `json:"id"`
		Asset    model.ObjectId   `json:"asset"`
		Username string           `json:"username"`
		Protocol model.LabelValue `json:"protocol"`
		IsActive bool             `json:"is_active"`
		Version  int              `json:"version"`
	}
	response, err := client.Get("/api/v1/accounts/personal-asset-credentials/"+req.AccountUsername+"/", &credential)
	if err != nil {
		return nil, personalCredentialRequestError(response, err)
	}
	if !strings.EqualFold(credential.ID, req.AccountUsername) || credential.Asset.ID != asset.ID ||
		!strings.EqualFold(credential.Protocol.Value, req.Protocol) || !credential.IsActive || credential.Version < 1 {
		return nil, errors.New("personal credential is inactive or does not match the selected asset and protocol")
	}
	account, err := personalCredentialAccount(detail.PermedAccounts, credential.Username)
	if err != nil {
		return nil, err
	}
	request := map[string]any{
		"asset":                       asset.ID,
		"account":                     account.Alias,
		"protocol":                    req.Protocol,
		"connect_method":              model.ProtocolSSH,
		"remote_addr":                 remoteAddr,
		"personal_credential_id":      credential.ID,
		"personal_credential_version": credential.Version,
	}
	var info model.ConnectTokenInfo
	response, err = client.Post("/api/v1/authentication/connection-token/", request, &info,
		map[string]string{"acl_stepwise": "true"})
	if err != nil {
		return nil, personalCredentialRequestError(response, err)
	}
	if info.ID == "" {
		return nil, errors.New("Core returned an empty connection token")
	}
	if terminal != nil {
		if err = completeConnectionACL(ctx, s.jmsService, info.ID, terminal, locale); err != nil {
			return nil, err
		}
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	token, err := sshcert.GetConnectTokenInfo(s.jmsService, info.ID, true)
	return &token, err
}

func personalCredentialAccount(accounts []model.PermAccount, username string) (model.PermAccount, error) {
	var matched []model.PermAccount
	for _, account := range accounts {
		if account.Alias == model.InputUser {
			return account, nil
		}
		if account.Username == username && (account.Alias == model.DynamicUser ||
			!account.HasSecret && !account.IsSSHCertificate() && common.IsUUID(account.Alias)) {
			matched = append(matched, account)
		}
	}
	if len(matched) != 1 {
		return model.PermAccount{}, errors.New("personal credential requires a unique permitted account matching its username")
	}
	return matched[0], nil
}

func personalCredentialRequestError(response *http.Response, err error) error {
	if response != nil && response.StatusCode == http.StatusUnauthorized {
		return errors.New("personal credential authentication expired; please reconnect to Koko")
	}
	return err
}

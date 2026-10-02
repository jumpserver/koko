package session

import (
	"regexp"

	"github.com/jumpserver/koko/pkg/lion/guacd"

	"github.com/jumpserver-dev/sdk-go/common"
	"github.com/jumpserver-dev/sdk-go/model"
)

type TunnelSession struct {
	ID             string                `json:"id"`
	Protocol       string                `json:"protocol"`
	Created        common.UTCTime        `json:"-"`
	Asset          *model.Asset          `json:"asset"`
	Account        *model.Account        `json:"-"`
	User           *model.User           `json:"user"`
	Platform       *model.Platform       `json:"platform"`
	RemoteApp      *model.Applet         `json:"remote_app"`
	Permission     *model.Permission     `json:"permission"`
	Gateway        *model.Gateway        `json:"-"`
	GatewayTarget  *model.Gateway        `json:"-"`
	TerminalConfig *model.TerminalConfig `json:"-"`
	ExpireInfo     model.ExpireInfo      `json:"expire_info"`
	ActionPerm     *ActionPermission     `json:"action_permission"`
	DisplayAccount *model.Account        `json:"system_user"`

	AppletOpts *model.AppletOption `json:"-"`
	AuthInfo   *model.ConnectToken `json:"-"`

	VirtualAppOpts *model.VirtualAppContainer `json:"-"`

	ConnectedCallback       func() error          `json:"-"`
	ConnectedFailedCallback func(err error) error `json:"-"`
	DisConnectedCallback    func() error          `json:"-"`

	ReleaseAppletAccount func() error `json:"-"`

	ModelSession *model.Session `json:"-"`
}

const (
	vnc = "vnc"
	rdp = "rdp"
)

func (s TunnelSession) GuaConfiguration() guacd.Configuration {
	if s.AppletOpts != nil {
		return s.configurationRemoteAppRDP()
	}
	if s.VirtualAppOpts != nil {
		return s.configurationVirtualApp()
	}
	switch s.Protocol {
	case vnc:
		return s.configurationVNC()
	default:
		return s.configurationRDP()
	}
}

func (s TunnelSession) configurationVNC() guacd.Configuration {
	conf := VNCConfiguration{
		SessionId:      s.ID,
		Created:        s.Created,
		User:           s.User,
		Asset:          s.Asset,
		Account:        s.Account,
		Platform:       s.Platform,
		TerminalConfig: s.TerminalConfig,
		ActionsPerm:    s.ActionPerm,
	}
	return conf.GetGuacdConfiguration()
}

func (s TunnelSession) configurationRDP() guacd.Configuration {
	if s.AppletOpts != nil {
		return s.configurationRemoteAppRDP()
	}
	rdpConf := RDPConfiguration{
		SessionId:      s.ID,
		Created:        s.Created,
		User:           s.User,
		Asset:          s.Asset,
		Account:        s.Account,
		Platform:       s.Platform,
		TerminalConfig: s.TerminalConfig,
		ActionsPerm:    s.ActionPerm,
	}
	return rdpConf.GetGuacdConfiguration()
}

// Core issues virtual usernames in this format for Tinker login tickets.
var appletLoginTicketUsername = regexp.MustCompile(`^jlt_[a-z2-7]{16}$`)

func isAppletLoginTicket(option *model.AppletOption) bool {
	return option != nil && appletLoginTicketUsername.MatchString(option.Account.Username)
}

func (s TunnelSession) configurationRemoteAppRDP() guacd.Configuration {
	appletOpt := s.AppletOpts
	rdpConf := RDPConfiguration{
		SessionId:      s.ID,
		Created:        s.Created,
		User:           s.User,
		Asset:          &appletOpt.Host,
		Account:        &appletOpt.Account,
		Platform:       s.Platform,
		TerminalConfig: s.TerminalConfig,
		ActionsPerm:    s.ActionPerm,
	}
	conf := rdpConf.GetGuacdConfiguration()
	if isAppletLoginTicket(appletOpt) {
		// Route the opaque ticket to the local Credential Provider, including on
		// domain members. Tinker's managed shell starts the app after redemption.
		conf.SetParameter(guacd.RDPDomain, "localhost")
		// Optional server-side NLA still permits CredSSP negotiation, which would
		// reject the virtual ticket before the Credential Provider can redeem it.
		conf.SetParameter(guacd.RDPSecurity, SecurityTls)
		return conf
	}
	remoteAPP := appletOpt.RemoteAppOption
	// 设置 remote app 参数
	{
		conf.SetParameter(guacd.RDPRemoteApp, remoteAPP.Name)
		conf.SetParameter(guacd.RDPRemoteAppDir, "")
		conf.SetParameter(guacd.RDPRemoteAppArgs, remoteAPP.CmdLine)
	}
	return conf
}

func (s TunnelSession) configurationVirtualApp() guacd.Configuration {
	vncConf := VirtualAppConfiguration{
		SessionId:      s.ID,
		Created:        s.Created,
		User:           s.User,
		VirtualAppOpt:  s.VirtualAppOpts,
		TerminalConfig: s.TerminalConfig,
		ActionsPerm:    s.ActionPerm,
	}
	return vncConf.GetGuacdConfiguration()
}

const (
	SecurityAny       = "any"
	SecurityNla       = "nla"
	SecurityNlaExt    = "nla-ext"
	SecurityTls       = "tls"
	SecurityVmConnect = "vmconnect"
	SecurityRdp       = "rdp"
)

func ValidateSecurityValue(security string) bool {
	switch security {
	case SecurityAny,
		SecurityNla,
		SecurityNlaExt,
		SecurityTls,
		SecurityVmConnect,
		SecurityRdp:
		return true
	}
	return false
}

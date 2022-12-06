package proxy

import (
	"fmt"

	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/srvconn"
)

type ConnectionOption func(options *ConnectionOptions)

func ConnectUser(user *model.User) ConnectionOption {
	return func(opts *ConnectionOptions) {
		opts.user = user
	}
}

func ConnectAsset(asset *model.Asset) ConnectionOption {
	return func(opts *ConnectionOptions) {
		opts.asset = asset
	}
}

func ConnectAccount(account *model.Account) ConnectionOption {
	return func(opts *ConnectionOptions) {
		opts.predefinedAccount = account
	}
}

func ConnectProtocol(protocol string) ConnectionOption {
	return func(opts *ConnectionOptions) {
		opts.Protocol = protocol
	}
}

func ConnectDomain(domain *model.Domain) ConnectionOption {
	return func(opts *ConnectionOptions) {
		opts.predefinedDomain = domain
	}
}

func ConnectActions(actions model.Actions) ConnectionOption {
	return func(opts *ConnectionOptions) {
		opts.predefinedActions = actions
	}
}

func ConnectPlatform(platform *model.Platform) ConnectionOption {
	return func(opts *ConnectionOptions) {
		opts.predefinedPlatform = platform
	}
}

func ConnectGateway(gateway *model.Gateway) ConnectionOption {
	return func(opts *ConnectionOptions) {
		opts.predefinedGateway = gateway
	}
}

func ConnectCmdACLRules(rules model.CommandACLs) ConnectionOption {
	return func(opts *ConnectionOptions) {
		opts.predefinedCmdACLRules = rules
	}
}

func ConnectExpired(expired model.ExpireInfo) ConnectionOption {
	return func(opts *ConnectionOptions) {
		opts.predefinedExpiredAt = expired
	}
}

func ConnectContainer(info *ContainerInfo) ConnectionOption {
	return func(opts *ConnectionOptions) {
		opts.k8sContainer = info
	}
}

func ConnectParams(params *ConnectionParams) ConnectionOption {
	return func(opts *ConnectionOptions) {
		opts.params = params
	}
}

func ConnectI18nLang(lang string) ConnectionOption {
	return func(opts *ConnectionOptions) {
		opts.i18nLang = lang
	}
}

type ConnectionOptions struct {
	Protocol string
	i18nLang string

	user  *model.User
	asset *model.Asset

	k8sContainer *ContainerInfo

	params *ConnectionParams

	predefinedExpiredAt   model.ExpireInfo
	predefinedGateway     *model.Gateway
	predefinedDomain      *model.Domain
	predefinedCmdACLRules model.CommandACLs
	predefinedAccount     *model.Account
	predefinedPlatform    *model.Platform
	predefinedActions     model.Actions
}

type ConnectionParams struct {
	DisableMySQLAutoHash bool
}

type ContainerInfo struct {
	Namespace string
	PodName   string
	Container string
}

func (c *ContainerInfo) String() string {
	return fmt.Sprintf("%s_%s_%s", c.Namespace, c.PodName, c.Container)
}

func (c *ContainerInfo) K8sName(name string) string {
	k8sName := fmt.Sprintf("%s(%s)", name, c.String())
	if len([]rune(k8sName)) <= 128 {
		return k8sName
	}
	containerName := []rune(c.String())
	nameRune := []rune(name)
	remainLen := 128 - len(nameRune) - 2 - 3
	indexLen := remainLen / 2
	startIndex := len(containerName) - indexLen
	startPart := string(containerName[:indexLen])
	endPart := string(containerName[startIndex:])
	return fmt.Sprintf("%s(%s...%s)", name, startPart, endPart)
}

func (opts *ConnectionOptions) TerminalTitle() string {
	title := ""
	switch opts.Protocol {
	case srvconn.ProtocolK8s:
		title = fmt.Sprintf("%s+%s",
			opts.Protocol,
			opts.asset.Address)
	default:
		title = fmt.Sprintf("%s://%s@%s",
			opts.Protocol,
			opts.predefinedAccount.Username,
			opts.asset.Address)
	}
	return title
}

func (opts *ConnectionOptions) ConnectMsg() string {
	lang := opts.getLang()
	msg := ""
	switch opts.Protocol {
	case srvconn.ProtocolTELNET,
		srvconn.ProtocolSSH:
		msg = fmt.Sprintf(lang.T("Connecting to %s@%s"), opts.predefinedAccount.Name, opts.asset.Address)
	case srvconn.ProtocolMySQL, srvconn.ProtocolMariadb, srvconn.ProtocolSQLServer,
		srvconn.ProtocolPostgreSQL, srvconn.ProtocolClickHouse,
		srvconn.ProtocolRedis, srvconn.ProtocolMongoDB:
		msg = fmt.Sprintf(lang.T("Connecting to Database %s"), opts.asset.String())
	case srvconn.ProtocolK8s:
		msg = fmt.Sprintf(lang.T("Connecting to Kubernetes %s"), opts.asset.Address)
		if opts.k8sContainer != nil {
			msg = fmt.Sprintf(lang.T("Connecting to Kubernetes %s container %s"),
				opts.asset.Name, opts.k8sContainer.Container)
		}
	}
	return msg
}

func (opts *ConnectionOptions) getLang() i18n.LanguageCode {
	return i18n.NewLang(opts.i18nLang)
}

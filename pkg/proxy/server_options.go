package proxy

import (
	"fmt"

	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/srvconn"
)

type ConnectionOption func(options *ConnectionOptions)

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

func ConnectTokenAuthInfo(authInfo *model.ConnectToken) ConnectionOption {
	return func(opts *ConnectionOptions) {
		opts.authInfo = authInfo
	}
}

type ConnectionOptions struct {
	authInfo *model.ConnectToken

	i18nLang string

	k8sContainer *ContainerInfo

	params *ConnectionParams
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
	protocol := opts.authInfo.Protocol
	asset := opts.authInfo.Asset
	account := opts.authInfo.Account

	title := ""
	switch protocol {
	case srvconn.ProtocolK8s:
		title = fmt.Sprintf("%s+%s",
			protocol,
			asset.Address)
	default:
		title = fmt.Sprintf("%s://%s@%s",
			protocol,
			account.Username,
			asset.Address)
	}
	return title
}

func (opts *ConnectionOptions) ConnectMsg() string {
	protocol := opts.authInfo.Protocol
	asset := opts.authInfo.Asset
	account := opts.authInfo.Account
	lang := opts.getLang()
	msg := ""
	switch protocol {
	case srvconn.ProtocolTELNET,
		srvconn.ProtocolSSH:
		accountName := account.String()
		switch account.Name {
		case model.InputUser:
			accountName = fmt.Sprintf("%s(%s)", lang.T("Manual"), account.Username)
		case model.DynamicUser:
			accountName = fmt.Sprintf("%s(%s)", lang.T("Dynamic"), account.Username)
		default:
		}
		msg = fmt.Sprintf(lang.T("Connecting to %s@%s"), accountName, asset.Address)
	case srvconn.ProtocolClickHouse,
		srvconn.ProtocolRedis, srvconn.ProtocolMongoDB,
		srvconn.ProtocolMySQL, srvconn.ProtocolSQLServer, srvconn.ProtocolPostgresql:
		msg = fmt.Sprintf(lang.T("Connecting to Database %s"), asset.String())
	case srvconn.ProtocolK8s:
		msg = fmt.Sprintf(lang.T("Connecting to Kubernetes %s"), asset.Address)
		if opts.k8sContainer != nil {
			msg = fmt.Sprintf(lang.T("Connecting to Kubernetes %s container %s"),
				asset.Name, opts.k8sContainer.Container)
		}
	}
	return msg
}

func (opts *ConnectionOptions) getLang() i18n.LanguageCode {
	return i18n.NewLang(opts.i18nLang)
}

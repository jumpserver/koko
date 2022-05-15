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

func ConnectProtocolType(protocol string) ConnectionOption {
	return func(opts *ConnectionOptions) {
		opts.ProtocolType = protocol
	}
}

func ConnectSystemUser(systemUser *model.SystemUser) ConnectionOption {
	return func(opts *ConnectionOptions) {
		opts.systemUser = systemUser
	}
}

func ConnectAsset(asset *model.Asset) ConnectionOption {
	return func(opts *ConnectionOptions) {
		opts.asset = asset
	}
}

func ConnectApp(app *model.Application) ConnectionOption {
	return func(opts *ConnectionOptions) {
		opts.app = app
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

func ConnectDomain(domain *model.Domain) ConnectionOption {
	return func(opts *ConnectionOptions) {
		opts.predefinedDomain = domain
	}
}

func ConnectPermission(perm *model.Permission) ConnectionOption {
	return func(opts *ConnectionOptions) {
		opts.predefinedPermission = perm
	}
}

func ConnectFilterRules(rules model.FilterRules) ConnectionOption {
	return func(opts *ConnectionOptions) {
		opts.predefinedCmdFilterRules = rules
	}
}

func ConnectExpired(expired int64) ConnectionOption {
	return func(opts *ConnectionOptions) {
		opts.predefinedExpiredAt = expired
	}
}

func ConnectSystemAuthInfo(info *model.SystemUserAuthInfo) ConnectionOption {
	return func(opts *ConnectionOptions) {
		opts.predefinedSystemUserAuthInfo = info
	}
}

type ConnectionOptions struct {
	ProtocolType string
	i18nLang     string

	user       *model.User
	systemUser *model.SystemUser

	asset *model.Asset

	app *model.Application

	k8sContainer *ContainerInfo

	params *ConnectionParams

	predefinedExpiredAt          int64
	predefinedPermission         *model.Permission
	predefinedDomain             *model.Domain
	predefinedCmdFilterRules     model.FilterRules
	predefinedSystemUserAuthInfo *model.SystemUserAuthInfo
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
	switch opts.ProtocolType {
	case srvconn.ProtocolTELNET,
		srvconn.ProtocolSSH:
		title = fmt.Sprintf("%s://%s@%s",
			opts.ProtocolType,
			opts.systemUser.Username,
			opts.asset.IP)
	case srvconn.ProtocolMySQL, srvconn.ProtocolMariadb, srvconn.ProtocolSQLServer,
		srvconn.ProtocolRedis, srvconn.ProtocolMongoDB:
		title = fmt.Sprintf("%s://%s@%s",
			opts.ProtocolType,
			opts.systemUser.Username,
			opts.app.Attrs.Host)
	case srvconn.ProtocolK8s:
		title = fmt.Sprintf("%s+%s",
			opts.ProtocolType,
			opts.app.Attrs.Cluster)
	}
	return title
}

func (opts *ConnectionOptions) ConnectMsg() string {
	lang := opts.getLang()
	msg := ""
	switch opts.ProtocolType {
	case srvconn.ProtocolTELNET,
		srvconn.ProtocolSSH:
		msg = fmt.Sprintf(lang.T("Connecting to %s@%s"), opts.systemUser.Name, opts.asset.IP)
	case srvconn.ProtocolMySQL, srvconn.ProtocolMariadb, srvconn.ProtocolSQLServer,
		srvconn.ProtocolRedis, srvconn.ProtocolMongoDB, srvconn.ProtocolPostgreSQL:
		msg = fmt.Sprintf(lang.T("Connecting to Database %s"), opts.app)
	case srvconn.ProtocolK8s:
		msg = fmt.Sprintf(lang.T("Connecting to Kubernetes %s"), opts.app.Attrs.Cluster)
		if opts.k8sContainer != nil {
			msg = fmt.Sprintf(lang.T("Connecting to Kubernetes %s container %s"),
				opts.app.Name, opts.k8sContainer.Container)
		}
	}
	return msg
}

func (opts *ConnectionOptions) getLang() i18n.LanguageCode {
	return i18n.NewLang(opts.i18nLang)
}

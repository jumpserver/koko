package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/service"
	"github.com/jumpserver/koko/pkg/srvconn"
	"github.com/jumpserver/koko/pkg/utils"
)

var _ proxyEngine = (*K8sProxyServer)(nil)

type K8sProxyServer struct {
	UserConn   UserConnection
	User       *model.User
	Cluster    *model.K8sApplication
	SystemUser *model.SystemUser
}

func (p *K8sProxyServer) checkProtocolMatch() bool {
	return strings.EqualFold(p.Cluster.TypeName, p.SystemUser.Protocol)
}

func (p *K8sProxyServer) checkProtocolClientInstalled() bool {
	switch strings.ToLower(p.Cluster.TypeName) {
	case "k8s":
		return IsInstalledKubectlClient()
	}
	return false
}

// validatePermission 检查是否有权限连接
func (p *K8sProxyServer) validatePermission() bool {
	return service.ValidateUserApplicationPermission(p.User.ID, p.Cluster.Id, p.SystemUser.ID)
}

// getSSHConn 获取ssh连接
func (p *K8sProxyServer) getK8sConConn(localAddr *net.TCPAddr) (srvConn *srvconn.K8sCon, err error) {
	clusterServer := p.Cluster.Attrs.Cluster
	if localAddr != nil {
		originUrl, err := url.Parse(clusterServer)
		if err != nil {
			return nil, err
		}
		clusterServer = ReplaceURLHostAndPort(originUrl, "127.0.0.1", localAddr.Port)
	}
	srvConn = srvconn.NewK8sCon(
		srvconn.K8sToken(p.SystemUser.Token),
		srvconn.K8sClusterServer(clusterServer),
		srvconn.K8sUsername(p.SystemUser.Username),
		srvconn.K8sSkipTls(true),
	)
	err = srvConn.Connect()
	return
}

// getServerConn 获取获取server连接
func (p *K8sProxyServer) getServerConn(localAddr *net.TCPAddr) (srvConn srvconn.ServerConnection, err error) {
	done := make(chan struct{})
	defer func() {
		utils.IgnoreErrWriteString(p.UserConn, "\r\n")
		close(done)
	}()
	go p.sendConnectingMsg(done)
	return p.getK8sConConn(localAddr)
}

// sendConnectingMsg 发送连接信息
func (p *K8sProxyServer) sendConnectingMsg(done chan struct{}) {
	delay := 0.1
	msg := fmt.Sprintf(i18n.T("Connecting to Kubernetes %s %.1f"), p.Cluster.Attrs.Cluster, delay)
	utils.IgnoreErrWriteString(p.UserConn, msg)
	for {
		select {
		case <-done:
			return
		default:
			delayS := fmt.Sprintf("%.1f", delay)
			data := strings.Repeat("\x08", len(delayS)) + delayS
			utils.IgnoreErrWriteString(p.UserConn, data)
			time.Sleep(100 * time.Millisecond)
			delay += 0.1
		}
	}
}

// preCheckRequisite 检查是否满足条件
func (p *K8sProxyServer) preCheckRequisite() (ok bool) {
	if !p.checkProtocolMatch() {
		msg := utils.WrapperWarn(i18n.T("System user <%s> and kubernetes <%s> protocol are inconsistent."))
		msg = fmt.Sprintf(msg, p.SystemUser.Username, p.Cluster.TypeName)
		utils.IgnoreErrWriteString(p.UserConn, msg)
		logger.Errorf("Conn[%s] checking protocol matched failed: %s", p.UserConn.ID(), msg)
		return
	}
	logger.Infof("Conn[%s] System user and k8s protocol matched", p.UserConn.ID())
	if !p.checkProtocolClientInstalled() {
		msg := utils.WrapperWarn(i18n.T("%s protocol client not installed."))
		msg = fmt.Sprintf(msg, p.Cluster.TypeName)
		utils.IgnoreErrWriteString(p.UserConn, msg)
		logger.Errorf("Conn[%s] %s", p.UserConn.ID(), msg)
		return
	}
	logger.Infof("Conn[%s] System user protocol %s supported", p.UserConn.ID(), p.SystemUser.Protocol)
	if !p.validatePermission() {
		msg := utils.WrapperWarn(i18n.T("You don't have permission login %s"))
		msg = fmt.Sprintf(msg, p.Cluster.Attrs.Cluster)
		utils.IgnoreErrWriteString(p.UserConn, msg)
		logger.Errorf("Conn[%s] get k8s %s permission failed", p.UserConn.ID(), p.Cluster.Attrs.Cluster)
		return
	}
	logger.Infof("Conn[%s] has permission to access k8s %s", p.UserConn.ID(), p.Cluster.Attrs.Cluster)
	if err := p.checkRequiredAuth(); err != nil {
		msg := utils.WrapperWarn(i18n.T("You get auth token failed"))
		utils.IgnoreErrWriteString(p.UserConn, msg)
		logger.Errorf("Conn[%s] get k8s %s auth info failed: %s", p.UserConn.ID(), p.Cluster.Attrs.Cluster, err)
		return
	}
	return true
}

func (p *K8sProxyServer) checkRequiredAuth() error {
	info := service.GetApplicationSystemUserAuthInfo(p.SystemUser.ID)
	if info.Token == "" {
		return errors.New("no auth token")
	}
	p.SystemUser.Token = info.Token
	logger.Infof("Conn[%s] get k8s %s auth info from JMS core success",
		p.UserConn.ID(), p.Cluster.Attrs.Cluster)
	return nil
}

// sendConnectErrorMsg 发送连接错误消息
func (p *K8sProxyServer) sendConnectErrorMsg(err error) {
	msg := fmt.Sprintf("Connect K8s %s error: %s\r\n", p.Cluster.Attrs.Cluster, err)
	utils.IgnoreErrWriteString(p.UserConn, msg)
	logger.Error(msg)
	token := p.SystemUser.Token
	if token != "" {
		tokenLen := len(token)
		showLen := tokenLen / 2
		hiddenLen := tokenLen - showLen
		msg2 := fmt.Sprintf("Try token: %s", token[:showLen]+strings.Repeat("*", hiddenLen))
		logger.Errorf(msg2)
	}
}

func (p *K8sProxyServer) createDomainGateway(domainId string) (*domainGateway, error) {
	domain := service.GetDomainGateways(domainId)
	if domain.ID == "" {
		return nil, errors.New("invalid domain")
	}
	clusterUrl, err := url.Parse(p.Cluster.Attrs.Cluster)
	if err != nil {
		logger.Errorf("K8s proxy parse %s url err: %s", p.Cluster.Attrs.Cluster, err)
		return nil, err
	}
	// URL host 是包含port的结果
	hostAndPort := strings.Split(clusterUrl.Host, ":")
	var (
		dstHost string
		dstPort int
	)
	dstHost = hostAndPort[0]
	switch len(hostAndPort) {
	case 2:
		dstPort, err = strconv.Atoi(hostAndPort[1])
		if err != nil {
			logger.Errorf("K8s proxy convert dst port %s err: %s", hostAndPort[1], err)
			return nil, err
		}
	default:
		switch clusterUrl.Scheme {
		case "https":
			dstPort = 443
		default:
			dstPort = 80
		}
	}
	dGateway := domainGateway{
		domain:  &domain,
		dstIP:   dstHost,
		dstPort: dstPort,
	}
	return &dGateway, nil
}

// Proxy 代理
func (p *K8sProxyServer) Proxy() {
	if !p.preCheckRequisite() {
		logger.Errorf("Conn[%s] Check requisite failed", p.UserConn.ID())
		return
	}
	logger.Infof("Conn[%s] checking pre requisite success", p.UserConn.ID())
	// 创建Session
	sw, ok := CreateCommonSwitch(p)
	logger.Info("Create Common Switch", ok)
	if !ok {
		msg := i18n.T("Create k8s session failed")
		msg = utils.WrapperWarn(msg)
		utils.IgnoreErrWriteString(p.UserConn, msg)
		logger.Error(msg)
		return
	}
	logger.Infof("Conn[%s] create k8s session %s success", p.UserConn.ID(), sw.ID)
	defer RemoveCommonSwitch(sw)
	var localTCPAddr *net.TCPAddr
	if p.Cluster.Domain != "" {
		dGateway, err := p.createDomainGateway(p.Cluster.Domain)
		if err != nil {
			msg := i18n.T("Create k8s domain gateway failed %s")
			msg = utils.WrapperWarn(fmt.Sprintf(msg, err))
			utils.IgnoreErrWriteString(p.UserConn, msg)
			return
		}
		localTCPAddr, err = dGateway.Start()
		if err != nil {
			msg := i18n.T("Start k8s domain gateway failed %s")
			msg = utils.WrapperWarn(fmt.Sprintf(msg, err))
			utils.IgnoreErrWriteString(p.UserConn, msg)
			return
		}
		defer dGateway.Stop()
	}
	srvConn, err := p.getServerConn(localTCPAddr)
	// 连接后端服务器失败
	if err != nil {
		logger.Errorf("Conn[%s] create k8s conn failed: %s", p.UserConn.ID(), err)
		p.sendConnectErrorMsg(err)
		return
	}
	logger.Infof("Conn[%s] get k8s conn success", p.UserConn.ID())
	_ = sw.Bridge(p.UserConn, srvConn)
	logger.Infof("Conn[%s] end k8s session %s bridge", p.UserConn.ID(), sw.ID)
}

func (p *K8sProxyServer) GenerateRecordCommand(s *commonSwitch, input, output string,
	riskLevel int64) *model.Command {
	return &model.Command{
		SessionID:  s.ID,
		OrgID:      p.Cluster.OrgId,
		Input:      input,
		Output:     output,
		User:       fmt.Sprintf("%s (%s)", p.User.Name, p.User.Username),
		Server:     fmt.Sprintf("%s (%s)", p.Cluster.Name, p.Cluster.Attrs.Cluster),
		SystemUser: fmt.Sprintf("%s (%s)", p.SystemUser.Name, p.SystemUser.Username),
		Timestamp:  time.Now().Unix(),
		RiskLevel:  riskLevel,

		DateCreated: time.Now(),
	}
}

func (p *K8sProxyServer) NewParser(s *commonSwitch) ParseEngine {
	shellParser := newParser(s.ID, p.SystemUser.Protocol)
	msg := i18n.T("Create k8s session failed")
	if cmdRules, err := service.GetSystemUserFilterRules(p.SystemUser.ID); err == nil {
		shellParser.SetCMDFilterRules(cmdRules)
	} else {
		msg = utils.WrapperWarn(msg)
		utils.IgnoreErrWriteString(p.UserConn, msg)
		logger.Error(msg + err.Error())
	}
	return &shellParser
}

func (p *K8sProxyServer) MapData(s *commonSwitch) map[string]interface{} {
	var dataEnd interface{}
	if s.DateEnd != "" {
		dataEnd = s.DateEnd
	}
	return map[string]interface{}{
		"id":             s.ID,
		"user":           fmt.Sprintf("%s (%s)", p.User.Name, p.User.Username),
		"asset":          p.Cluster.Name,
		"org_id":         p.Cluster.OrgId,
		"login_from":     p.UserConn.LoginFrom(),
		"system_user":    fmt.Sprintf("%s (%s)", p.SystemUser.Name, p.SystemUser.Username),
		"protocol":       p.SystemUser.Protocol,
		"remote_addr":    p.UserConn.RemoteAddr(),
		"is_finished":    s.finished,
		"date_start":     s.DateStart,
		"date_end":       dataEnd,
		"user_id":        p.User.ID,
		"asset_id":       p.Cluster.Id,
		"system_user_id": p.SystemUser.ID,
		"is_success":     s.isConnected,
	}
}

func IsInstalledKubectlClient() bool {
	checkLine := "kubectl version --client -o json"
	cmd := exec.Command("bash", "-c", checkLine)
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		logger.Errorf("Check kubectl client failed %s", err)
		return false
	}
	var result map[string]interface{}
	err = json.Unmarshal(out, &result)
	if err != nil {
		logger.Errorf("Check kubectl client failed %s %s", err, out)
		return false
	}
	if _, ok := result["clientVersion"]; ok {
		return true
	}
	logger.Errorf("Check kubectl client failed: %s", out)
	return false
}

func ReplaceURLHostAndPort(originUrl *url.URL, ip string, port int) string {
	newHost := net.JoinHostPort(ip, strconv.Itoa(port))
	switch originUrl.Scheme {
	case "https":
		if port == 443 {
			newHost = ip
		}
	default:
		if port == 80 {
			newHost = ip
		}
	}
	newUrl := url.URL{
		Scheme:     originUrl.Scheme,
		Opaque:     originUrl.Opaque,
		User:       originUrl.User,
		Host:       newHost,
		Path:       originUrl.Path,
		RawPath:    originUrl.RawQuery,
		ForceQuery: originUrl.ForceQuery,
		RawQuery:   originUrl.RawQuery,
		Fragment:   originUrl.Fragment,
	}
	return newUrl.String()
}

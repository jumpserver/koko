package handler

import (
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"

	"github.com/gliderlabs/ssh"
	"golang.org/x/term"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/service"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/proxy"
	"github.com/jumpserver/koko/pkg/srvconn"
	"github.com/jumpserver/koko/pkg/utils"
)

/*
直接连接资产使用的登录名，支持使用以下格式：

1. JMS_username[@mysql|ssh|redis]@account_username@asset_target
2. JMS_username[#mysql|ssh|redis]#account_username#asset_target

JMS_username: 			JumpServer 平台上的用户名
account_username：	    对应账号的用户名
asset_target: 			对应资产的ip 或者 id

FormatNORMAL: 使用 account_username 和 asset_ip 的登录方式，即1和2的方式

FormatToken:  使用 JMS-{token} 的方式登陆方式

*/

type FormatType int

const (
	FormatNORMAL FormatType = iota
	FormatToken
)

type DirectOpt func(*directOpt)

type directOpt struct {
	formatType    FormatType
	protocol      string
	targetAccount string
	User          *model.User
	terminalConf  *model.TerminalConfig

	tokenInfo *model.ConnectToken

	sftpMode bool

	assets []model.Asset
}

func (d directOpt) IsTokenConnection() bool {
	return d.formatType == FormatToken
}

func DirectAssets(assets []model.Asset) DirectOpt {
	return func(opts *directOpt) {
		opts.assets = assets
	}
}

func DirectTargetAccount(username string) DirectOpt {
	return func(opts *directOpt) {
		opts.targetAccount = username
	}
}

func DirectUser(User *model.User) DirectOpt {
	return func(opts *directOpt) {
		opts.User = User
	}
}

func DirectTerminalConf(conf *model.TerminalConfig) DirectOpt {
	return func(opts *directOpt) {
		opts.terminalConf = conf
	}
}

func DirectFormatType(format FormatType) DirectOpt {
	return func(opts *directOpt) {
		opts.formatType = format
	}
}

func DirectConnectToken(tokenInfo *model.ConnectToken) DirectOpt {
	return func(opts *directOpt) {
		opts.tokenInfo = tokenInfo
	}
}

func DirectConnectProtocol(protocol string) DirectOpt {
	return func(opts *directOpt) {
		opts.protocol = protocol
	}
}

func DirectConnectSftpMode(sftpMode bool) DirectOpt {
	return func(opts *directOpt) {
		opts.sftpMode = sftpMode
	}
}

func NewDirectHandler(sess ssh.Session, jmsService *service.JMService, setters ...DirectOpt) *DirectHandler {
	opts := &directOpt{}
	for i := range setters {
		setters[i](opts)
	}
	i18nLang := getUserDefaultLangCode(opts.User)
	var (
		wrapperSess *WrapperSession
		termVt      *term.Terminal
	)

	if !opts.sftpMode {
		wrapperSess = NewWrapperSession(sess)
		termVt = term.NewTerminal(wrapperSess, "Opt> ")
	}
	d := &DirectHandler{
		opts:       opts,
		sess:       sess,
		jmsService: jmsService,
		i18nLang:   i18nLang,

		wrapperSess: wrapperSess,
		term:        termVt,
	}
	return d

}

type DirectHandler struct {
	term        *term.Terminal
	sess        ssh.Session
	wrapperSess *WrapperSession
	opts        *directOpt
	jmsService  *service.JMService

	i18nLang string

	selectAsset   *model.Asset
	selectAccount *model.PermAccount
}

func (d *DirectHandler) NewSFTPHandler() *SftpHandler {
	addr, _, _ := net.SplitHostPort(d.sess.RemoteAddr().String())
	opts := make([]srvconn.UserSftpOption, 0, 5)
	opts = append(opts, srvconn.WithUser(d.opts.User))
	opts = append(opts, srvconn.WithRemoteAddr(addr))
	opts = append(opts, srvconn.WithLoginFrom(model.LoginFromSSH))
	if !d.opts.IsTokenConnection() {
		opts = append(opts, srvconn.WithAssets(d.opts.assets))
	} else {
		opts = append(opts, srvconn.WithConnectToken(d.opts.tokenInfo))
	}
	return &SftpHandler{
		UserSftpConn: srvconn.NewUserSftpConn(d.jmsService, opts...),
		recorder:     proxy.GetFTPFileRecorder(d.jmsService),
	}
}

func (d *DirectHandler) Dispatch() {
	_, winChan, _ := d.sess.Pty()
	go d.WatchWinSizeChange(winChan)
	if d.opts.IsTokenConnection() {
		d.LoginConnectToken(d.opts.tokenInfo)
		return
	}
	d.LoginAsset()
}

func (d *DirectHandler) GetPtyWinSize() (width, height int) {
	pty := d.wrapperSess.Pty()
	return pty.Window.Width, pty.Window.Height
}

func (d *DirectHandler) WatchWinSizeChange(winChan <-chan ssh.Window) {
	defer logger.Infof("Request %s: Windows change watch close", d.wrapperSess.Uuid)
	for {
		select {
		case <-d.sess.Context().Done():
			return
		case win, ok := <-winChan:
			if !ok {
				return
			}
			d.wrapperSess.SetWin(win)
			logger.Debugf("Term window size change: %d*%d", win.Height, win.Width)
			_ = d.term.SetSize(win.Width, win.Height)
		}
	}
}

func (d *DirectHandler) LoginAsset() {
	switch len(d.opts.assets) {
	case 1:
		d.Proxy(d.opts.assets[0])
	default:
		checkChan := make(chan bool)
		go d.checkMaxIdleTime(checkChan)
		for {
			d.displayAssets(d.opts.assets)
			checkChan <- true
			num, err := d.term.ReadLine()
			if err != nil {
				logger.Error(err)
				return
			}
			checkChan <- false
			if indexNum, err2 := strconv.Atoi(num); err2 == nil && len(d.opts.assets) > 0 {
				if indexNum > 0 && indexNum <= len(d.opts.assets) {
					d.Proxy(d.opts.assets[indexNum-1])
					return
				}
			}
			switch num {
			case "q", "quit", "exit":
				logger.Infof("User %s enter %s to exit ", d.opts.User, num)
				return
			}
		}
	}
}

func (d *DirectHandler) checkMaxIdleTime(checkChan chan bool) {
	maxIdleMinutes := d.opts.terminalConf.MaxIdleTime
	checkMaxIdleTime(maxIdleMinutes, d.i18nLang, d.opts.User,
		d.sess, checkChan)
}

func (d *DirectHandler) chooseAccount(permAccounts []model.PermAccount) (model.PermAccount, bool) {
	lang := i18n.NewLang(d.i18nLang)
	length := len(permAccounts)
	switch length {
	case 0:
		warningInfo := lang.T("No Account found.")
		_, _ = io.WriteString(d.term, warningInfo+"\n\r")
		return model.PermAccount{}, false
	case 1:
		return permAccounts[0], true
	default:
	}
	displayAccounts := model.PermAccountList(permAccounts)
	sort.Sort(displayAccounts)

	idLabel := lang.T("ID")
	nameLabel := lang.T("Name")
	usernameLabel := lang.T("Username")

	labels := []string{idLabel, nameLabel, usernameLabel}
	fields := []string{"ID", "Name", "Username"}

	data := make([]map[string]string, len(displayAccounts))
	for i, j := range displayAccounts {
		row := make(map[string]string)
		row["ID"] = strconv.Itoa(i + 1)
		row["Name"] = j.Name
		row["Username"] = j.Username
		data[i] = row
	}
	w, _ := d.GetPtyWinSize()
	table := common.WrapperTable{
		Fields: fields,
		Labels: labels,
		FieldsSize: map[string][3]int{
			"ID":       {0, 0, 5},
			"Name":     {0, 8, 0},
			"Username": {0, 10, 0},
		},
		Data:        data,
		TotalSize:   w,
		TruncPolicy: common.TruncMiddle,
	}
	table.Initial()

	d.term.SetPrompt("ID> ")
	selectTip := fmt.Sprintf(lang.T("Tips: Enter asset[%s] account ID"), d.selectAsset.String())
	backTip := lang.T("Back: B/b")
	for {
		utils.IgnoreErrWriteString(d.term, table.Display())
		utils.IgnoreErrWriteString(d.term, utils.WrapperString(selectTip, utils.Green))
		utils.IgnoreErrWriteString(d.term, utils.CharNewLine)
		utils.IgnoreErrWriteString(d.term, utils.WrapperString(backTip, utils.Green))
		utils.IgnoreErrWriteString(d.term, utils.CharNewLine)
		line, err := d.term.ReadLine()
		if err != nil {
			logger.Errorf("select account err: %s", err)
			return model.PermAccount{}, false
		}
		line = strings.TrimSpace(line)
		switch strings.ToLower(line) {
		case "q", "b", "quit", "exit", "back":
			logger.Info("select account cancel")
			return model.PermAccount{}, false
		}
		if num, err2 := strconv.Atoi(line); err2 == nil {
			if num > 0 && num <= len(displayAccounts) {
				return displayAccounts[num-1], true
			}
		} else {
			logger.Errorf("select account not right number %s", line)
			return model.PermAccount{}, false
		}
	}
}

func (d *DirectHandler) displayAssets(assets []model.Asset) {
	assetListSortBy := d.opts.terminalConf.AssetListSortBy
	model.AssetList(assets).SortBy(assetListSortBy)

	vt := d.term
	lang := i18n.NewLang(d.i18nLang)
	idLabel := lang.T("ID")
	hostLabel := lang.T("Hostname")
	ipLabel := lang.T("Address")
	commentLabel := lang.T("Comment")

	Labels := []string{idLabel, hostLabel, ipLabel, commentLabel}
	fields := []string{"ID", "Hostname", "Address", "Comment"}
	data := make([]map[string]string, len(assets))
	for i := range assets {
		row := make(map[string]string)
		row["ID"] = strconv.Itoa(i + 1)
		row["Hostname"] = assets[i].Name
		row["Address"] = assets[i].Address
		row["Comment"] = joinMultiLineString(assets[i].Comment)
		data[i] = row
	}
	w, _ := d.GetPtyWinSize()

	table := common.WrapperTable{
		Fields: fields,
		Labels: Labels,
		FieldsSize: map[string][3]int{
			"ID":       {0, 0, 5},
			"Hostname": {0, 40, 0},
			"Address":  {0, 15, 40},
			"Comment":  {0, 0, 0},
		},
		Data:        data,
		TotalSize:   w,
		TruncPolicy: common.TruncMiddle,
	}
	table.Initial()
	loginTip := lang.T("select one asset to login")

	_, _ = vt.Write([]byte(utils.CharClear))
	_, _ = vt.Write([]byte(table.Display()))
	utils.IgnoreErrWriteString(vt, utils.WrapperString(loginTip, utils.Green))
	utils.IgnoreErrWriteString(vt, utils.CharNewLine)
}

func (d *DirectHandler) Proxy(asset model.Asset) {
	d.selectAsset = &asset
	lang := i18n.NewLang(d.i18nLang)
	accounts, err := d.jmsService.GetAccountsByUserIdAndAssetId(d.opts.User.ID, asset.ID)
	if err != nil {
		logger.Errorf("Get account failed: %s", err)
		utils.IgnoreErrWriteString(d.term, lang.T("Core API failed"))
		return
	}
	matched := GetMatchedAccounts(accounts, d.opts.targetAccount)
	if len(matched) == 0 {
		msg := fmt.Sprintf(lang.T("not found matched username %s"), d.opts.targetAccount)
		utils.IgnoreErrWriteString(d.term, msg+"\r\n")
		logger.Errorf("Get account failed: %s", msg)
		return
	}
	selectAccount, ok := d.chooseAccount(matched)
	if !ok {
		logger.Info("Do not select account")
		return
	}
	d.selectAccount = &selectAccount
	protocol := d.opts.protocol
	req := service.SuperConnectTokenReq{
		UserId:        d.opts.User.ID,
		AssetId:       asset.ID,
		Account:       selectAccount.Alias,
		Protocol:      protocol,
		ConnectMethod: model.ProtocolSSH,
	}
	tokenInfo, err := d.jmsService.CreateSuperConnectToken(&req)
	if err != nil {
		if tokenInfo.Code == "" {
			logger.Errorf("Create connect token and auth info failed: %s", err)
			utils.IgnoreErrWriteString(d.term, lang.T("Core API failed"))
			return
		}
		switch tokenInfo.Code {
		case model.ACLReject:
			logger.Errorf("Create connect token and auth info failed: %s", tokenInfo.Detail)
			utils.IgnoreErrWriteString(d.term, lang.T("ACL reject"))
			utils.IgnoreErrWriteString(d.term, utils.CharNewLine)
			return
		case model.ACLReview:
			reviewHandler := LoginReviewHandler{
				readWriter: d.wrapperSess,
				i18nLang:   d.i18nLang,
				user:       d.opts.User,
				jmsService: d.jmsService,
				req:        &req,
			}
			ok2, err2 := reviewHandler.WaitReview(d.sess.Context())
			if err2 != nil {
				logger.Errorf("Wait login review failed: %s", err)
				utils.IgnoreErrWriteString(d.term, lang.T("Core API failed"))
				return
			}
			if !ok2 {
				logger.Error("Wait login review failed")
				return
			}
			tokenInfo = reviewHandler.tokenInfo
		default:
			logger.Errorf("Create connect token and auth info failed: %s", tokenInfo.Detail)
			return
		}
	}
	connectToken, err := d.jmsService.GetConnectTokenInfo(tokenInfo.ID)
	if err != nil {
		logger.Errorf("Create connect token and auth info failed: %s", err)
		utils.IgnoreErrWriteString(d.term, lang.T("get connect token err"))
		return
	}
	d.LoginConnectToken(&connectToken)
}

func GetMatchedAccounts(accounts []model.PermAccount, username string) []model.PermAccount {
	matched := make([]model.PermAccount, 0, len(accounts))
	for i := range accounts {
		account := accounts[i]
		if account.Username == username {
			matched = append(matched, account)
		}
	}
	return matched
}

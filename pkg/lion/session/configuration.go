package session

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/viper"

	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/lion/guacd"

	"github.com/jumpserver-dev/sdk-go/common"
	"github.com/jumpserver-dev/sdk-go/model"
)

type ConnectionConfiguration interface {
	GetGuacdConfiguration() guacd.Configuration
}

var (
	_ ConnectionConfiguration = RDPConfiguration{}
	_ ConnectionConfiguration = VNCConfiguration{}
	_ ConnectionConfiguration = VirtualAppConfiguration{}
)

type RDPConfiguration struct {
	SessionId      string
	Created        common.UTCTime
	User           *model.User
	Asset          *model.Asset
	Account        *model.Account
	Platform       *model.Platform
	TerminalConfig *model.TerminalConfig
	ActionsPerm    *ActionPermission
}

func (r RDPConfiguration) GetGuacdConfiguration() guacd.Configuration {
	var (
		username string
		password string
		ip       string
		port     string
		adDomain string
	)

	ip = r.Asset.Address
	port = strconv.Itoa(r.Asset.ProtocolPort(rdp))
	username = r.Account.Username
	password = r.Account.Secret

	conf := guacd.NewConfiguration()
	conf.Protocol = rdp
	conf.SetParameter(guacd.Hostname, ip)
	conf.SetParameter(guacd.Port, port)

	/*
		 pam 会处理 ad Domain 的信息，转化成 username@domain 的格式
		 不在从 platform 处理
			if r.Platform != nil {
				if rdpSetting, ok := r.Platform.GetProtocolSetting(rdp); ok {
					if rdpSetting.Setting.AdDomain != "" {
						adDomain = rdpSetting.Setting.AdDomain
					}
				}
			}

			/*
				AD Domain 的处理调整为
				1、如果账号 username 格式是 domain\username 则需要转换为 username@domain，且覆盖平台的 AD 域设置。
				2、其他格式的账号，如果平台中设置了 AD 域则使用平台中的设置，否则使用不设置
	*/

	parts := strings.Split(username, `\`)
	if len(parts) == 2 {
		username = fmt.Sprintf("%s@%s", parts[1], parts[0])
		adDomain = parts[0]
	}

	// 试图从 username@domain 格式的 username 中获取 AD 域的信息
	//if adDomain == "" && strings.Contains(username, `@`) {
	//	adParts := strings.Split(username, `@`)
	//	if len(adParts) >= 2 {
	//		adDomain = adParts[len(adParts)-1]
	//	}
	//}
	// domain 和 用户名 同时设置 AD 域的信息，freerdp 会连接失败
	// domain 和 用户名 无法同时添加 AD 域 的信息

	conf.SetParameter(guacd.RDPUsername, username)
	conf.SetParameter(guacd.RDPPassword, password)
	if adDomain != "" {
		conf.SetParameter(guacd.RDPDomain, adDomain)
	}

	//// 设置 录像路径
	//if r.TerminalConfig.ReplayStorage.TypeName != "null" {
	//	recordDirPath := filepath.Join(config.GlobalConfig.RecordPath,
	//		r.Created.Format(recordDirTimeFormat))
	//	conf.SetParameter(guacd.RecordingPath, recordDirPath)
	//	conf.SetParameter(guacd.CreateRecordingPath, BoolTrue)
	//	conf.SetParameter(guacd.RecordingName, r.SessionId)
	//}

	// display 相关
	{
		for key, value := range RDPDisplay.GetDisplayParams() {
			conf.SetParameter(key, value)
		}
		for key, value := range RDPBuiltIn {
			conf.SetParameter(key, value)
		}
		// reconnect 会造成创建多个录像文件
		conf.SetParameter(guacd.RDPResizeMethod, "display-update")
	}

	// 设置 挂载目录 上传下载
	{
		driverShareId := r.User.ID
		if config.GetConf().DriveScope == config.DriverScopeSession {
			driverShareId = r.SessionId
		}
		drivePath := filepath.Join(config.GetConf().DrivePath, driverShareId)
		enableDrive := ConvertBoolToString(r.ActionsPerm.EnableDownload || r.ActionsPerm.EnableUpload)
		disableDownload := ConvertBoolToString(!r.ActionsPerm.EnableDownload)
		disableUpload := ConvertBoolToString(!r.ActionsPerm.EnableUpload)
		conf.SetParameter(guacd.RDPDrivePath, drivePath)
		conf.SetParameter(guacd.RDPCreateDrivePath, BoolTrue)
		conf.SetParameter(guacd.RDPEnableDrive, enableDrive)
		conf.SetParameter(guacd.RDPDriveName, "Lion")
		conf.SetParameter(guacd.RDPDisableDownload, disableDownload)
		conf.SetParameter(guacd.RDPDisableUpload, disableUpload)
	}

	// 粘贴复制
	{
		disableCopy := ConvertBoolToString(!r.ActionsPerm.EnableCopy)
		disablePaste := ConvertBoolToString(!r.ActionsPerm.EnablePaste)
		conf.SetParameter(guacd.DisableCopy, disableCopy)
		conf.SetParameter(guacd.DisablePaste, disablePaste)
	}

	// 平台中的设置
	rdpSecurityValue := SecurityAny
	if r.Platform != nil {
		if rdpSettings, ok := r.Platform.GetProtocolSetting(rdp); ok {
			setObj := rdpSettings.GetSetting()
			if setObj.Security != "" {
				rdpSecurityValue = setObj.Security
			}
			if setObj.Console {
				conf.SetParameter(guacd.RDPConsole, BoolTrue)
			}
		}
	}
	conf.SetParameter(guacd.RDPSecurity, rdpSecurityValue)
	conf.SetParameter(guacd.RDPIgnoreCert, BoolTrue)

	// 设置客户端名称，任务管理器--用户---客户端名称显示
	conf.SetParameter(guacd.RDPClientName, "Lion")

	return conf
}

type VNCConfiguration struct {
	SessionId      string
	Created        common.UTCTime
	User           *model.User
	Asset          *model.Asset
	Account        *model.Account
	Platform       *model.Platform
	TerminalConfig *model.TerminalConfig
	ActionsPerm    *ActionPermission
}

const recordDirTimeFormat = "2006-01-02"

const nullUsername = "null"

func (r VNCConfiguration) GetGuacdConfiguration() guacd.Configuration {
	conf := guacd.NewConfiguration()
	var (
		username string
		password string
		ip       string
		port     string
	)
	ip = r.Asset.Address
	port = strconv.Itoa(r.Asset.ProtocolPort("vnc"))
	username = r.Account.Username
	password = r.Account.Secret
	if username == nullUsername {
		username = ""
	}
	conf.Protocol = vnc
	conf.SetParameter(guacd.Hostname, ip)
	conf.SetParameter(guacd.Port, port)

	{
		conf.SetParameter(guacd.VNCUsername, username)
		conf.SetParameter(guacd.VNCPassword, password)
		conf.SetParameter(guacd.VNCAutoretry, "3")
	}
	// 设置存储
	//replayCfg := r.TerminalConfig.ReplayStorage
	//if replayCfg.TypeName != "null" {
	//	recordDirPath := filepath.Join(config.GlobalConfig.RecordPath, r.Created.Format(recordDirTimeFormat))
	//	conf.SetParameter(guacd.RecordingPath, recordDirPath)
	//	conf.SetParameter(guacd.CreateRecordingPath, BoolTrue)
	//	conf.SetParameter(guacd.RecordingName, r.SessionId)
	//}
	{
		for key, value := range VNCDisplay.GetDisplayParams() {
			conf.SetParameter(key, value)
		}
	}

	// 粘贴复制
	{
		disableCopy := ConvertBoolToString(!r.ActionsPerm.EnableCopy)
		disablePaste := ConvertBoolToString(!r.ActionsPerm.EnablePaste)
		conf.SetParameter(guacd.DisableCopy, disableCopy)
		conf.SetParameter(guacd.DisablePaste, disablePaste)
	}

	// VNC_CLIPBOARD_ENCODING from env
	if value := viper.GetString("VNC_CLIPBOARD_ENCODING"); value != "" {
		conf.SetParameter(guacd.VNCClipboardEncoding, value)
	}

	return conf
}

const (
	BoolFalse = "false"
	BoolTrue  = "true"
)

func ConvertBoolToString(b bool) string {
	if b {
		return BoolTrue
	}
	return BoolFalse
}

type VirtualAppConfiguration struct {
	SessionId      string
	Created        common.UTCTime
	User           *model.User
	VirtualAppOpt  *model.VirtualAppContainer
	TerminalConfig *model.TerminalConfig
	ActionsPerm    *ActionPermission
}

func (r VirtualAppConfiguration) GetGuacdConfiguration() guacd.Configuration {
	conf := guacd.NewConfiguration()
	var (
		username string
		password string
		ip       string
		port     string
	)
	ip = r.VirtualAppOpt.Host
	port = strconv.Itoa(r.VirtualAppOpt.Port)
	username = r.VirtualAppOpt.Username
	password = r.VirtualAppOpt.Password
	sftpPort := r.VirtualAppOpt.SFTPPort
	conf.Protocol = vnc
	conf.SetParameter(guacd.Hostname, ip)
	conf.SetParameter(guacd.Port, port)

	{
		conf.SetParameter(guacd.VNCUsername, username)
		conf.SetParameter(guacd.VNCPassword, password)
		conf.SetParameter(guacd.VNCAutoretry, "10")
	}
	// 设置存储
	//replayCfg := r.TerminalConfig.ReplayStorage
	//if replayCfg.TypeName != "null" {
	//	recordDirPath := filepath.Join(config.GlobalConfig.RecordPath, r.Created.Format(recordDirTimeFormat))
	//	conf.SetParameter(guacd.RecordingPath, recordDirPath)
	//	conf.SetParameter(guacd.CreateRecordingPath, BoolTrue)
	//	conf.SetParameter(guacd.RecordingName, r.SessionId)
	//}
	{
		for key, value := range VNCDisplay.GetDisplayParams() {
			conf.SetParameter(key, value)
		}
	}

	// 粘贴复制
	{
		disableCopy := ConvertBoolToString(!r.ActionsPerm.EnableCopy)
		disablePaste := ConvertBoolToString(!r.ActionsPerm.EnablePaste)
		conf.SetParameter(guacd.DisableCopy, disableCopy)
		conf.SetParameter(guacd.DisablePaste, disablePaste)
	}
	// vnc 强制使用 utf8 编码
	conf.SetParameter(guacd.VNCClipboardEncoding, "UTF-8")

	if sftpPort > 0 {
		//  sftp enable and set sftp username and password
		enableDrive := ConvertBoolToString(r.ActionsPerm.EnableDownload || r.ActionsPerm.EnableUpload)
		disableDownload := ConvertBoolToString(!r.ActionsPerm.EnableDownload)
		disableUpload := ConvertBoolToString(!r.ActionsPerm.EnableUpload)
		conf.SetParameter(guacd.EnableSftp, enableDrive)
		conf.SetParameter(guacd.SftpHostname, ip)
		conf.SetParameter(guacd.SftpPort, strconv.Itoa(sftpPort))
		conf.SetParameter(guacd.SftpUsername, vAPPSFTPUsername)
		conf.SetParameter(guacd.SftpPassword, password)
		conf.SetParameter(guacd.SftpRootDirectory, sftpRootDir)
		conf.SetParameter(guacd.SftpDisableDownload, disableDownload)
		conf.SetParameter(guacd.SftpDisableUpload, disableUpload)
	}
	return conf
}

const (
	vAPPSFTPUsername = "jumpserver"
	sftpRootDir      = "/tmp/jumpserver/download"
)

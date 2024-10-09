package koko

import (
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/exchange"
	"github.com/jumpserver/koko/pkg/httpd"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/sshd"

	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/service"
)

type Koko struct {
	webSrv *httpd.Server
	sshSrv *sshd.Server
}

func (k *Koko) Start() {
	go k.webSrv.Start()
	go k.sshSrv.Start()
}

func (k *Koko) Stop() {
	k.sshSrv.Stop()
	k.webSrv.Stop()
	logger.Info("Quit The KoKo")
}

func RunForever(confPath string) {
	config.Setup(confPath)
	bootstrap()
	gracefulStop := make(chan os.Signal, 1)
	signal.Notify(gracefulStop, syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT)
	jmsService := MustJMService()
	webSrv := httpd.NewServer(jmsService)
	sshSrv := sshd.NewSSHServer(jmsService)
	app := &Koko{
		webSrv: webSrv,
		sshSrv: sshSrv,
	}
	app.Start()
	runTasks(jmsService)
	<-gracefulStop
	app.Stop()
}

func bootstrap() {
	i18n.Initial()
	logger.Initial()
	exchange.Initial()
}

func runTasks(jmsService *service.JMService) {
	if config.GetConf().UploadFailedReplay {
		go uploadRemainReplay(jmsService)
	}
	if config.GetConf().UploadFailedFTPFile {
		go uploadRemainFTPFile(jmsService)
	}
	go keepHeartbeat(jmsService)

	go RunConnectTokensCheck(jmsService)
}

func MustJMService() *service.JMService {
	key := MustLoadValidAccessKey()
	jmsService, err := service.NewAuthJMService(service.JMSCoreHost(
		config.GlobalConfig.CoreHost), service.JMSTimeOut(30*time.Second),
		service.JMSAccessKey(key.ID, key.Secret),
	)
	if err != nil {
		logger.Fatal("创建JMS Service 失败 " + err.Error())
		os.Exit(1)
	}
	return jmsService
}

func MustLoadValidAccessKey() model.AccessKey {
	conf := config.GlobalConfig
	var key model.AccessKey
	if err := key.LoadFromFile(conf.AccessKeyFilePath); err != nil {
		return MustRegisterTerminalAccount()
	}
	// 校验accessKey
	return MustValidKey(key)
}

func MustRegisterTerminalAccount() (key model.AccessKey) {
	conf := config.GlobalConfig
	for i := 0; i < 10; i++ {
		terminal, err := service.RegisterTerminalAccount(conf.CoreHost,
			conf.Name, conf.BootstrapToken)
		if err != nil {
			logger.Error(err.Error())
			time.Sleep(5 * time.Second)
			continue
		}
		key.ID = terminal.ServiceAccount.AccessKey.ID
		key.Secret = terminal.ServiceAccount.AccessKey.Secret
		if err := key.SaveToFile(conf.AccessKeyFilePath); err != nil {
			logger.Error("保存key失败: " + err.Error())
		}
		return key
	}
	logger.Error("注册终端失败退出")
	os.Exit(1)
	return
}

func MustValidKey(key model.AccessKey) model.AccessKey {
	conf := config.GlobalConfig
	for i := 0; i < 10; i++ {
		if err := service.ValidAccessKey(conf.CoreHost, key); err != nil {
			switch {
			case errors.Is(err, service.ErrUnauthorized):
				logger.Error("Access key unauthorized, try to register new access key")
				return MustRegisterTerminalAccount()
			default:
				logger.Error("校验 access key failed: " + err.Error())
			}
			time.Sleep(5 * time.Second)
			continue
		}
		return key
	}
	logger.Error("校验 access key failed退出")
	os.Exit(1)
	return key
}

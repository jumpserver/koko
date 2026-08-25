package koko

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/exchange"
	"github.com/jumpserver/koko/pkg/httpd"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/lion"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/sshd"
	"github.com/jumpserver/koko/pkg/terminalai"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
)

type Koko struct {
	webSrv     *httpd.Server
	sshSrv     *sshd.Server
	lion       *lion.Runtime
	appContext context.Context
	cancel     context.CancelFunc
}

func (k *Koko) Start() {
	go k.webSrv.Start()
	go k.sshSrv.Start()
	k.lion.Start(k.appContext)
}

func (k *Koko) Stop() {
	k.webSrv.Stop()
	k.sshSrv.Stop()
	if err := exchange.Close(); err != nil {
		logger.Errorf("Close exchange manager failed: %s", err)
	}
	k.cancel()
	k.lion.Stop()
	logger.Info("Quit The KoKo")
}

func RunForever(confPath string) {
	config.Setup(confPath)
	bootstrap()
	aiResult, err := terminalai.Configure(
		context.Background(),
		terminalai.Configuration{
			RulesFile: config.GetConf().TerminalAIRulesFile,
		},
	)
	if err != nil {
		_, _ = terminalai.Configure(
			context.Background(),
			terminalai.Configuration{},
		)
		logger.Errorf(
			"Load Terminal AI business rules failed; using built-in rules: %s",
			err,
		)
	} else if aiResult.RuleCount > 0 {
		logger.Infof(
			"Loaded %d Terminal AI business rules",
			aiResult.RuleCount,
		)
	}
	jmsService := MustJMService()
	gracefulStop := make(chan os.Signal, 1)
	signal.Notify(gracefulStop, syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT)
	bootstrapWithJMService(jmsService)
	lionRuntime := lion.NewRuntime(jmsService)
	webSrv := httpd.NewServer(jmsService, lionRuntime)
	sshSrv := sshd.NewSSHServer(jmsService)
	appContext, cancel := context.WithCancel(context.Background())
	app := &Koko{
		webSrv:     webSrv,
		sshSrv:     sshSrv,
		lion:       lionRuntime,
		appContext: appContext,
		cancel:     cancel,
	}
	app.Start()
	runTasks(jmsService, lionRuntime)
	<-gracefulStop
	app.Stop()
}

func bootstrap() {
	i18n.Initial()
	logger.Initial()
}

func bootstrapWithJMService(jmsService *service.JMService) {
	updateEncryptConfigValue(jmsService)
	exchange.Initial()
}

func updateEncryptConfigValue(jmsService *service.JMService) {
	cfg := config.GlobalConfig
	encryptKey := cfg.SecretEncryptKey
	if encryptKey != "" {
		redisPassword := cfg.RedisPassword
		ret, err := jmsService.GetEncryptedConfigValue(encryptKey, redisPassword)
		if err != nil {
			logger.Error("Get encrypted config value failed: " + err.Error())
			return
		}
		if ret.Value != "" {
			cfg.UpdateRedisPassword(ret.Value)
		} else {
			logger.Error("Get encrypted config value failed: empty value")
		}
	}
}

func runTasks(jmsService *service.JMService, lionRuntime *lion.Runtime) {
	if config.GetConf().UploadFailedReplay {
		go uploadRemainReplay(jmsService)
	}
	if config.GetConf().UploadFailedFTPFile {
		go uploadRemainFTPFile(jmsService)
	}
	go keepHeartbeat(jmsService, lionRuntime)

	go RunConnectTokensCheck(jmsService)
}

func MustJMService() *service.JMService {
	key := MustLoadValidAccessKey()
	jmsService, err := service.NewAuthJMService(
		service.JMSCoreHost(config.GlobalConfig.CoreHost),
		service.JMSTimeOut(time.Duration(config.GlobalConfig.HttpRequestTimeout)*time.Second),
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
		terminal, err := service.RegisterTerminalAccount(conf.CoreHost, string(model.Koko),
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

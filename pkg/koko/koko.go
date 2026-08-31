package koko

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jumpserver/koko/internal/agent"
	"github.com/jumpserver/koko/internal/agentauth"
	"github.com/jumpserver/koko/internal/agenthttp"
	"github.com/jumpserver/koko/internal/agentruntime/provider"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/exchange"
	"github.com/jumpserver/koko/pkg/httpd"
	"github.com/jumpserver/koko/pkg/i18n"
	"github.com/jumpserver/koko/pkg/lion"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/sshd"
	"github.com/jumpserver/koko/pkg/webproxy"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
)

type Koko struct {
	webSrv     *httpd.Server
	sshSrv     *sshd.Server
	lion       *lion.Runtime
	webProxy   *webproxy.Server
	agent      *agenthttp.Server
	appContext context.Context
	cancel     context.CancelFunc
}

func (k *Koko) Start() {
	go k.webSrv.Start()
	go k.sshSrv.Start()
	if k.webProxy != nil {
		go k.webProxy.Start()
	}
	if k.agent != nil {
		go func() {
			logger.Infof("Koko agent runtime listening on %s with API prefix /koko/agent/", k.agent.Addr())
			if err := k.agent.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Errorf("Koko agent runtime stopped: %s", err)
			}
		}()
	}
	k.lion.Start(k.appContext)
}

func (k *Koko) Stop() {
	if k.agent != nil {
		if err := k.agent.BeginShutdown(); err != nil {
			logger.Errorf("Persist Koko agent shutdown state failed: %s", err)
		}
	}
	k.webSrv.Stop()
	k.sshSrv.Stop()
	if k.webProxy != nil {
		k.webProxy.Stop()
	}
	if k.agent != nil {
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
		if err := k.agent.Shutdown(shutdownCtx); err != nil {
			logger.Errorf("Stop Koko agent HTTP listener failed: %s", err)
		}
		cancelShutdown()
	}
	if k.agent != nil {
		k.agent.Close()
	}
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
	jmsService := MustJMService()
	gracefulStop := make(chan os.Signal, 1)
	signal.Notify(gracefulStop, syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT)
	bootstrapWithJMService(jmsService)
	lionRuntime := lion.NewRuntime(jmsService)
	webSrv := httpd.NewServer(jmsService, lionRuntime)
	sshSrv := sshd.NewSSHServer(jmsService)
	var webProxySrv *webproxy.Server
	if conf := config.GetConf(); conf.WebProxyEnabled {
		var err error
		recordingRoot := ""
		if conf.WebProxyRecordingEnabled {
			recordingRoot = filepath.Join(conf.ReplayFolderPath, "web")
		}
		webProxySrv, err = webproxy.NewServer(
			conf.WebProxyBindHost,
			conf.WebProxyPort,
			conf.WebProxyAllowedHosts,
			recordingRoot,
			conf.WebProxyFFmpegPath,
			jmsService,
		)
		if err != nil {
			logger.Fatalf("Invalid Web proxy configuration: %s", err)
		}
	}
	appContext, cancel := context.WithCancel(context.Background())
	agentServer := newAgentServer(jmsService)
	app := &Koko{
		webSrv:     webSrv,
		sshSrv:     sshSrv,
		lion:       lionRuntime,
		webProxy:   webProxySrv,
		agent:      agentServer,
		appContext: appContext,
		cancel:     cancel,
	}
	app.Start()
	runTasks(jmsService, lionRuntime)
	<-gracefulStop
	app.Stop()
}

func newAgentServer(jmsService *service.JMService) *agenthttp.Server {
	conf := config.GetConf()
	if !conf.AgentEnabled {
		logger.Info("Koko agent runtime is disabled")
		return nil
	}
	verifier := &agentauth.JMServiceVerifier{Service: jmsService}
	agentService, err := agent.New(agent.Options{
		DataDir:    filepath.Join(conf.DataFolderPath, "agent"),
		InstanceID: conf.Name,
		ModelFactory: func() (provider.Provider, error) {
			var terminalConfig agent.TerminalConfig
			if _, callErr := jmsService.Call(
				"GET", service.TerminalConfigURL, nil, &terminalConfig,
			); callErr != nil {
				return nil, fmt.Errorf("load agent model configuration: %w", callErr)
			}
			return provider.New(agent.ProviderConfigFromTerminalConfig(terminalConfig))
		},
	})
	if err != nil {
		logger.Errorf("Initialize Koko agent runtime failed; terminal service remains available: %s", err)
		return nil
	}
	server, err := agenthttp.New(agenthttp.Options{
		Addr:    net.JoinHostPort(conf.BindHost, conf.AgentHTTPPort),
		Service: agentService,
		Authenticator: &agentauth.CoreAuthenticator{
			Cookies: verifier,
			Headers: verifier,
		},
		OriginVerifier:    &agentauth.SameOriginVerifier{},
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
	})
	if err != nil {
		agentService.Close()
		logger.Errorf("Initialize Koko agent HTTP server failed; terminal service remains available: %s", err)
		return nil
	}
	return server
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

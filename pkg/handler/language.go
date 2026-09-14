package handler

import (
	"strings"
	"time"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/i18n"
)

func getUserDefaultLangCode(user *model.User) string {
	if user.Language != "" {
		return user.Language
	}
	return config.GetConf().LanguageCode
}

// newLangAPIClient 复制一份会话独占的 API client，避免设置语言时影响其他用户
func newLangAPIClient(jmsService *service.JMService, langCode string) *service.JMService {
	apiClient := jmsService.Copy()
	setAPIClientLang(apiClient, langCode)
	return apiClient
}

func newUserAPIClient(token, langCode string) (*service.JMService, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, nil
	}
	conf := config.GetConf()
	opts := []service.Option{
		service.JMSCoreHost(conf.CoreHost),
		service.JMSTimeOut(time.Duration(conf.HttpRequestTimeout) * time.Second),
	}
	if conf.IgnoreVerifyCerts {
		opts = append(opts, service.JMSInsecure())
	}
	client, err := service.NewAuthJMService(opts...)
	if err != nil {
		return nil, err
	}
	client.SetHeader("Authorization", "Bearer "+token)
	setAPIClientLang(client, langCode)
	return client, nil
}

func setAPIClientLang(jmsService *service.JMService, langCode string) {
	coreCode := i18n.NewLang(langCode).CoreCode()
	jmsService.SetHeader("Accept-Language", coreCode)
	jmsService.SetCookie("django_language", coreCode)
}

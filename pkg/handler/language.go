package handler

import (
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

func setAPIClientLang(jmsService *service.JMService, langCode string) {
	coreCode := i18n.NewLang(langCode).CoreCode()
	jmsService.SetHeader("Accept-Language", coreCode)
	jmsService.SetCookie("django_language", coreCode)
}

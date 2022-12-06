package service

import (
	"os"
	"testing"

	"github.com/jumpserver/koko/pkg/jms-sdk-go/httplib"
)

func setup() *JMService {
	/*
		从环境变量获取 认证信息
		CORE_HOST
		access_key_id
		access_key_secret
	*/
	auth := httplib.SigAuth{
		KeyID:    os.Getenv("access_key_id"),
		SecretID: os.Getenv("access_key_secret"),
	}
	jms, err := NewAuthJMService(JMSAccessKey(auth.KeyID, auth.SecretID),
		JMSCoreHost(os.Getenv("CORE_HOST")))
	if err != nil {
		panic(err)
	}
	return jms
}

func TestJMService_GetProfile(t *testing.T) {
	jms := setup()
	user, err := jms.GetProfile()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%+v", user)
}

func TestJMService_GetTerminalConfig(t *testing.T) {
	jms := setup()
	conf, err := jms.GetTerminalConfig()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%+v", conf)
}

func TestJMService_GetAssetById(t *testing.T) {
	jms := setup()
	assetIds := []string{
		"2e73f0e4-13ec-4f64-b03e-4ecbadab7233", // 有网域
		"bd87e0b9-9a94-48df-9fa1-4aab4c9f49a5", // 无网域
	}
	for i := range assetIds {
		asset, err := jms.GetAssetDetailById(assetIds[i])
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%+v\n", asset)
	}

}

func TestJMService_GetDomainGateways(t *testing.T) {
	jms := setup()
	domains := []string{
		"aad81461-5f10-40f6-9064-ed6de855d0c7",
	}
	for i := range domains {
		asset, err := jms.GetDomainGateways(domains[i])
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%+v\n", asset)
	}
}

func TestJMService_SubmitCommandConfirm(t *testing.T) {
	jms := setup()
	sid := "8e7df6b6-795c-4904-bd17-f3bf2855ae9f"
	ruleId := "0fd1112f-1c14-4457-bff8-62e21b1a64a2"
	command := "ls"
	res, err := jms.SubmitCommandConfirm(sid, ruleId, command)
	t.Log(res, err)
}

func TestJMService_GetPublicSetting(t *testing.T) {
	jms := setup()
	setting, err := jms.GetPublicSetting()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%+v\n", setting)
}

package service

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"

	"cocogo/pkg/common"
)

type AccessKey struct {
	Id     string
	Secret string
}

func (ak *AccessKey) Signature(date string) string {
	signature := common.MakeSignature(ak.Secret, date)
	return fmt.Sprintf("Sign %s:%s", ak.Id, signature)
}

// LoadAccessKey 加载AccessKey用来与 Core Api 交互
func (s *Service) LoadAccessKey() {
	/*
	   1. 查看配置文件是否包含accessKey，解析不正确则退出程序
	   2. 检查是否已经注册过accessKey，
	   		1）已经注册过则进行解析，解析不正确则退出程序
	   		2）未注册则新注册
	*/
	if s.Conf.AccessKey != "" {
		fmt.Println(s.Conf.AccessKey)
		keyAndSecret := strings.Split(s.Conf.AccessKey, ":")
		if len(keyAndSecret) == 2 {
			s.auth = accessAuth{
				accessKey:    keyAndSecret[0],
				accessSecret: keyAndSecret[1],
			}
		} else {
			fmt.Println("ACCESS_KEY format err")
			os.Exit(1)
		}
		return
	}
	var configPath string

	if !path.IsAbs(s.Conf.AccessKeyFile) {
		configPath = filepath.Join(s.Conf.RootPath, s.Conf.AccessKeyFile)
	} else {
		configPath = s.Conf.AccessKeyFile
	}
	_, err := os.Stat(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("Do not have access key, register it!")
			err := s.registerTerminalAndSave()
			if err != nil {
				log.Info("register Failed:", err)
				os.Exit(1)
			}
			return
		} else {
			fmt.Println("sys err:", err)
			os.Exit(1)
		}

	}

	buf, err := ioutil.ReadFile(configPath)
	if err != nil {
		fmt.Println("read Access key Failed:", err)
		os.Exit(1)
	}
	keyAndSecret := strings.Split(string(buf), ":")
	if len(keyAndSecret) == 2 {
		s.auth = accessAuth{
			accessKey:    keyAndSecret[0],
			accessSecret: keyAndSecret[1],
		}
	} else {
		fmt.Println("ACCESS_KEY format err")
		os.Exit(1)
	}

}

package config

import (
	"fmt"
	log "github.com/sirupsen/logrus"
	"io/ioutil"
	"os"

	"gopkg.in/yaml.v2"
)

type Config struct {
	Name              string `yaml:"NAME"`
	CoreHost          string `yaml:"CORE_HOST"`
	BootstrapToken    string `yaml:"BOOTSTRAP_TOKEN"`
	BindHost          string `yaml:"BIND_HOST"`
	SshPort           int    `yaml:"SSHD_PORT"`
	HTTPPort          int    `yaml:"HTTPD_PORT"`
	CustomerAccessKey string `yaml:"ACCESS_KEY"`
	AccessKeyFile     string `yaml:"ACCESS_KEY_FILE"`
	LogLevel          string `yaml:"LOG_LEVEL"`
	HeartBeat         int    `yaml:"HEARTBEAT_INTERVAL"`
	RootPath          string
	Comment           string
	TermConfig        *TerminalConfig
}


func LoadFromYaml(filepath string) *Config {
	c := createDefaultConfig()
	body, err := ioutil.ReadFile(filepath)
	if err != nil {
		log.Errorf("Not found file: %s", filepath)
		os.Exit(1)
	}
	e := yaml.Unmarshal(body, &c)
	if e != nil {
		fmt.Println("Load yaml err")
		os.Exit(1)
	}
	return &c

}

func createDefaultConfig() Config {
	name, _ := os.Hostname()
	rootPath, _ := os.Getwd()
	return Config{
		Name:              name,
		CoreHost:          "http://localhost:8080",
		BootstrapToken:    "",
		BindHost:          "0.0.0.0",
		SshPort:           2222,
		HTTPPort:          5000,
		CustomerAccessKey: "",
		AccessKeyFile:     "data/keys/.access_key",
		LogLevel:          "DEBUG",
		RootPath:          rootPath,
		Comment:           "Coco",
		TermConfig:        &TerminalConfig{},
	}
}



type TerminalConfig struct {
	AssetListPageSize   string  `json:"TERMINAL_ASSET_LIST_PAGE_SIZE"`
	AssetListSortBy     string  `json:"TERMINAL_ASSET_LIST_SORT_BY"`
	CommandStorage      Storage `json:"TERMINAL_COMMAND_STORAGE"`
	HeaderTitle         string  `json:"TERMINAL_HEADER_TITLE"`
	HeartBeatInterval   int     `json:"TERMINAL_HEARTBEAT_INTERVAL"`
	HostKey             string  `json:"TERMINAL_HOST_KEY"`
	PasswordAuth        bool    `json:"TERMINAL_PASSWORD_AUTH"`
	PublicKeyAuth       bool    `json:"TERMINAL_PUBLIC_KEY_AUTH"`
	RePlayStorage       Storage `json:"TERMINAL_REPLAY_STORAGE"`
	SessionKeepDuration int     `json:"TERMINAL_SESSION_KEEP_DURATION"`
	TelnetRegex         string  `json:"TERMINAL_TELNET_REGEX"`
	SecurityMaxIdleTime int     `json:"SECURITY_MAX_IDLE_TIME"`
}

type Storage struct {
	Type string `json:"TYPE"`
}

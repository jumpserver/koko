package config

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"sync"

	"gopkg.in/yaml.v2"
)

type Config struct {
	AssetListPageSize   string                 `json:"TERMINAL_ASSET_LIST_PAGE_SIZE"`
	AssetListSortBy     string                 `json:"TERMINAL_ASSET_LIST_SORT_BY"`
	HeaderTitle         string                 `json:"TERMINAL_HEADER_TITLE"`
	HostKey             string                 `json:"TERMINAL_HOST_KEY" yaml:"HOST_KEY"`
	PasswordAuth        bool                   `json:"TERMINAL_PASSWORD_AUTH" yaml:"PASSWORD_AUTH"`
	PublicKeyAuth       bool                   `json:"TERMINAL_PUBLIC_KEY_AUTH" yaml:"PUBLIC_KEY_AUTH"`
	CommandStorage      map[string]interface{} `json:"TERMINAL_COMMAND_STORAGE"`
	ReplayStorage       map[string]interface{} `json:"TERMINAL_REPLAY_STORAGE" yaml:"REPLAY_STORAGE"`
	SessionKeepDuration int                    `json:"TERMINAL_SESSION_KEEP_DURATION"`
	TelnetRegex         string                 `json:"TERMINAL_TELNET_REGEX"`
	MaxIdleTime         int                    `json:"SECURITY_MAX_IDLE_TIME"`
	SftpRoot            string                 `json:"TERMINAL_SFTP_ROOT" yaml:"SFTP_ROOT"`
	Name                string                 `yaml:"NAME"`
	SecretKey           string                 `yaml:"SECRET_KEY"`
	HostKeyFile         string                 `yaml:"HOST_KEY_FILE"`
	CoreHost            string                 `yaml:"CORE_HOST"`
	BootstrapToken      string                 `yaml:"BOOTSTRAP_TOKEN"`
	BindHost            string                 `yaml:"BIND_HOST"`
	SSHPort             int                    `yaml:"SSHD_PORT"`
	HTTPPort            int                    `yaml:"HTTPD_PORT"`
	SSHTimeout          int                    `yaml:"SSH_TIMEOUT"`
	AccessKey           string                 `yaml:"ACCESS_KEY"`
	AccessKeyFile       string                 `yaml:"ACCESS_KEY_FILE"`
	LogLevel            string                 `yaml:"LOG_LEVEL"`
	HeartbeatDuration   int                    `yaml:"HEARTBEAT_INTERVAL"`
	RootPath            string                 `yaml:"ROOT_PATH"`
	Comment             string                 `yaml:"COMMENT"`
	Language            string                 `yaml:"LANG"`
	LanguageCode        string                 `yaml:"LANGUAGE_CODE"` // Abandon
	UploadFailedReplay  bool                   `yaml:"UPLOAD_FAILED_REPLAY_ON_START"`
}

func (c *Config) EnsureConfigValid() {
	// 兼容原来config
	if c.LanguageCode != "" && c.Language == "" {
		c.Language = c.LanguageCode
	}
	// 确保至少有一个认证
	if !c.PublicKeyAuth && !c.PasswordAuth {
		c.PasswordAuth = true
	}
}

func (c *Config) LoadFromYAML(body []byte) error {
	err := yaml.Unmarshal(body, c)
	if err != nil {
		log.Printf("Load yaml error: %v", err)
	}
	return err
}

func (c *Config) LoadFromYAMLPath(filepath string) error {
	body, err := ioutil.ReadFile(filepath)
	if err != nil {
		log.Printf("Not found file: %s", filepath)
	}
	return c.LoadFromYAML(body)
}

func (c *Config) LoadFromJSON(body []byte) error {
	err := json.Unmarshal(body, c)
	if err != nil {
		log.Printf("Config load yaml error")
	}
	return nil
}

func (c *Config) LoadFromEnv() error {
	envMap := map[string]string{}
	env := os.Environ()
	for _, v := range env {
		vSlice := strings.Split(v, "=")
		envMap[vSlice[0]] = envMap[vSlice[1]]
	}
	envYAML, err := yaml.Marshal(envMap)
	if err != nil {
		log.Fatalf("Error occur: %v", err)
	}
	return c.LoadFromYAML(envYAML)
}

func (c *Config) Load(filepath string) error {
	err := c.LoadFromYAMLPath(filepath)
	if err != nil {
		return err
	}
	err = c.LoadFromEnv()
	return err
}

var lock = new(sync.RWMutex)
var name, _ = os.Hostname()
var rootPath, _ = os.Getwd()
var Conf = &Config{
	Name:               name,
	CoreHost:           "http://localhost:8080",
	BootstrapToken:     "",
	BindHost:           "0.0.0.0",
	SSHPort:            2222,
	SSHTimeout:         15,
	HTTPPort:           5000,
	HeartbeatDuration:  10,
	AccessKey:          "",
	AccessKeyFile:      "data/keys/.access_key",
	LogLevel:           "DEBUG",
	HostKeyFile:        "data/keys/host_key",
	HostKey:            "",
	RootPath:           rootPath,
	Comment:            "Coco",
	Language:           "zh",
	ReplayStorage:      map[string]interface{}{"TYPE": "server"},
	CommandStorage:     map[string]interface{}{"TYPE": "server"},
	UploadFailedReplay: true,
}

func SetConf(conf *Config) {
	lock.Lock()
	defer lock.Unlock()
	Conf = conf
}

func GetConf() *Config {
	lock.RLock()
	defer lock.RUnlock()
	var conf Config
	if confBytes, err := json.Marshal(Conf); err == nil {
		_ = json.Unmarshal(confBytes, &conf)
	}
	return &conf
}

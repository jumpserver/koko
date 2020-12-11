package config

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v2"
)

var CipherKey = "JumpServer Cipher Key for KoKo !"

type Config struct {
	AssetListPageSize   string                 `json:"TERMINAL_ASSET_LIST_PAGE_SIZE"`
	AssetListSortBy     string                 `json:"TERMINAL_ASSET_LIST_SORT_BY"`
	HeaderTitle         string                 `json:"TERMINAL_HEADER_TITLE"`
	HostKey             string                 `json:"TERMINAL_HOST_KEY" yaml:"HOST_KEY"`
	PasswordAuth        bool                   `json:"TERMINAL_PASSWORD_AUTH" yaml:"PASSWORD_AUTH"`
	PublicKeyAuth       bool                   `json:"TERMINAL_PUBLIC_KEY_AUTH" yaml:"PUBLIC_KEY_AUTH"`
	CommandStorage      map[string]interface{} `json:"TERMINAL_COMMAND_STORAGE"`
	ReplayStorage       map[string]interface{} `json:"TERMINAL_REPLAY_STORAGE" yaml:"REPLAY_STORAGE"`
	SessionKeepDuration time.Duration          `json:"TERMINAL_SESSION_KEEP_DURATION"`
	TelnetRegex         string                 `json:"TERMINAL_TELNET_REGEX"`
	MaxIdleTime         time.Duration          `json:"SECURITY_MAX_IDLE_TIME"`
	HeartbeatDuration   time.Duration          `json:"TERMINAL_HEARTBEAT_INTERVAL"`
	ShowHiddenFile      bool                   `yaml:"SFTP_SHOW_HIDDEN_FILE"`
	ReuseConnection     bool                   `yaml:"REUSE_CONNECTION"`
	Name                string                 `yaml:"NAME"`
	HostKeyFile         string                 `yaml:"HOST_KEY_FILE"`
	CoreHost            string                 `yaml:"CORE_HOST"`
	BootstrapToken      string                 `yaml:"BOOTSTRAP_TOKEN"`
	BindHost            string                 `yaml:"BIND_HOST"`
	SSHPort             string                 `yaml:"SSHD_PORT"`
	HTTPPort            string                 `yaml:"HTTPD_PORT"`
	SSHTimeout          time.Duration          `yaml:"SSH_TIMEOUT"`
	AccessKey           string                 `yaml:"ACCESS_KEY"`
	AccessKeyFile       string                 `yaml:"ACCESS_KEY_FILE"`
	LogLevel            string                 `yaml:"LOG_LEVEL"`
	RootPath            string                 `yaml:"ROOT_PATH"`
	Comment             string                 `yaml:"COMMENT"`
	LanguageCode        string                 `yaml:"LANGUAGE_CODE"`
	UploadFailedReplay  bool                   `yaml:"UPLOAD_FAILED_REPLAY_ON_START"`
	AssetLoadPolicy     string                 `yaml:"ASSET_LOAD_POLICY"` // all
	ZipMaxSize          string                 `yaml:"ZIP_MAX_SIZE"`
	ZipTmpPath          string                 `yaml:"ZIP_TMP_PATH"`
	ClientAliveInterval uint64                 `yaml:"CLIENT_ALIVE_INTERVAL"`
	RetryAliveCountMax  int                    `yaml:"RETRY_ALIVE_COUNT_MAX"`

	ShareRoomType string   `yaml:"SHARE_ROOM_TYPE"`
	RedisHost     string   `yaml:"REDIS_HOST"`
	RedisPort     string   `yaml:"REDIS_PORT"`
	RedisPassword string   `yaml:"REDIS_PASSWORD"`
	RedisDBIndex  uint64   `yaml:"REDIS_DB_ROOM"`
	RedisClusters []string `yaml:"REDIS_CLUSTERS"`
}

func (c *Config) EnsureConfigValid() {
	if c.LanguageCode == "" {
		c.LanguageCode = "zh"
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
		return err
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
	envMap := make(map[string]string)
	env := os.Environ()
	for _, v := range env {
		vSlice := strings.Split(v, "=")
		key := vSlice[0]
		value := vSlice[1]
		// 环境变量的值，非字符串类型的解析，需要另作处理
		switch key {
		case "SFTP_SHOW_HIDDEN_FILE", "REUSE_CONNECTION", "UPLOAD_FAILED_REPLAY_ON_START":
			switch strings.ToLower(value) {
			case "true", "on":
				switch key {
				case "SFTP_SHOW_HIDDEN_FILE":
					c.ShowHiddenFile = true
				case "REUSE_CONNECTION":
					c.ReuseConnection = true
				case "UPLOAD_FAILED_REPLAY_ON_START":
					c.UploadFailedReplay = true
				}
			case "false", "off":
				switch key {
				case "SFTP_SHOW_HIDDEN_FILE":
					c.ShowHiddenFile = false
				case "REUSE_CONNECTION":
					c.ReuseConnection = false
				case "UPLOAD_FAILED_REPLAY_ON_START":
					c.UploadFailedReplay = false
				}
			}
		case "SSH_TIMEOUT":
			if num, err := strconv.Atoi(value); err == nil {
				c.SSHTimeout = time.Duration(num)
			}
		case "REDIS_CLUSTERS":
			clusters := strings.Split(value, ",")
			c.RedisClusters = clusters
		default:
			envMap[key] = value
		}
	}
	envYAML, err := yaml.Marshal(&envMap)
	if err != nil {
		log.Fatalf("Error occur: %v", err)
	}
	return c.LoadFromYAML(envYAML)
}

func (c *Config) Load(filepath string) error {
	var err error
	log.Print("Config Load from env first")
	_ = c.LoadFromEnv()
	if _, err = os.Stat(filepath); err == nil {
		log.Printf("Config reload from file: %s", filepath)
		return c.LoadFromYAMLPath(filepath)
	}
	return nil
}

var lock = new(sync.RWMutex)
var name = getDefaultName()
var rootPath, _ = os.Getwd()
var Conf = &Config{
	Name:                name,
	CoreHost:            "http://localhost:8080",
	BootstrapToken:      "",
	BindHost:            "0.0.0.0",
	SSHPort:             "2222",
	SSHTimeout:          15,
	HTTPPort:            "5000",
	HeartbeatDuration:   10,
	AccessKey:           "",
	AccessKeyFile:       "data/keys/.access_key",
	LogLevel:            "INFO",
	HostKeyFile:         "data/keys/host_key",
	HostKey:             "",
	RootPath:            rootPath,
	Comment:             "Coco",
	ReplayStorage:       map[string]interface{}{"TYPE": "server"},
	CommandStorage:      map[string]interface{}{"TYPE": "server"},
	UploadFailedReplay:  true,
	ShowHiddenFile:      false,
	ReuseConnection:     true,
	AssetLoadPolicy:     "",
	ZipMaxSize:          "1024M",
	ZipTmpPath:          "/tmp",
	ClientAliveInterval: 30,
	RetryAliveCountMax:  3,
	ShareRoomType:       "local",
	RedisHost:           "127.0.0.1",
	RedisPort:           "6379",
	RedisPassword:       "",
}

func SetConf(conf Config) {
	lock.Lock()
	defer lock.Unlock()
	Conf = &conf
}

func GetConf() Config {
	lock.RLock()
	defer lock.RUnlock()
	return *Conf
}

const prefixName = "[KoKo]"

func getDefaultName() string {
	hostname, _ := os.Hostname()
	hostRune := []rune(prefixName + hostname)
	if len(hostRune) <= 32 {
		return string(hostRune)
	}
	name := make([]rune, 32)
	copy(name[:16], hostRune[:16])
	start := len(hostRune) - 16
	copy(name[16:], hostRune[start:])
	return string(name)
}

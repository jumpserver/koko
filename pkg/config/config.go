package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

var (
	CipherKey = "JumpServer Cipher Key for KoKo !"

	KubectlBanner = "Welcome to JumpServer kubectl, try kubectl --help."
)

type Config struct {
	Name           string `mapstructure:"NAME"`
	CoreHost       string `mapstructure:"CORE_HOST"`
	BootstrapToken string `mapstructure:"BOOTSTRAP_TOKEN"`
	BindHost       string `mapstructure:"BIND_HOST"`
	SSHPort        string `mapstructure:"SSHD_PORT"`
	HTTPPort       string `mapstructure:"HTTPD_PORT"`
	SSHTimeout     int    `mapstructure:"SSH_TIMEOUT"`

	LogLevel string `mapstructure:"LOG_LEVEL"`

	Comment             string `mapstructure:"COMMENT"`
	LanguageCode        string `mapstructure:"LANGUAGE_CODE"`
	UploadFailedReplay  bool   `mapstructure:"UPLOAD_FAILED_REPLAY_ON_START"`
	AssetLoadPolicy     string `mapstructure:"ASSET_LOAD_POLICY"` // all
	ZipMaxSize          string `mapstructure:"ZIP_MAX_SIZE"`
	ZipTmpPath          string `mapstructure:"ZIP_TMP_PATH"`
	ClientAliveInterval int    `mapstructure:"CLIENT_ALIVE_INTERVAL"`
	RetryAliveCountMax  int    `mapstructure:"RETRY_ALIVE_COUNT_MAX"`
	ShowHiddenFile      bool   `mapstructure:"SFTP_SHOW_HIDDEN_FILE"`
	ReuseConnection     bool   `mapstructure:"REUSE_CONNECTION"`

	ShareRoomType string   `mapstructure:"SHARE_ROOM_TYPE"`
	RedisHost     string   `mapstructure:"REDIS_HOST"`
	RedisPort     string   `mapstructure:"REDIS_PORT"`
	RedisPassword string   `mapstructure:"REDIS_PASSWORD"`
	RedisDBIndex  int      `mapstructure:"REDIS_DB_ROOM"`
	RedisClusters []string `mapstructure:"REDIS_CLUSTERS"`

	EnableLocalPortForward   bool `mapstructure:"ENABLE_LOCAL_PORT_FORWARD"`
	EnableReversePortForward bool `mapstructure:"ENABLE_REVERSE_PORT_FORWARD"`
	EnableIDESupport         bool `mapstructure:"ENABLE_IDE_SUPPORT"`

	RootPath          string
	DataFolderPath    string
	LogDirPath        string
	KeyFolderPath     string
	AccessKeyFilePath string
	ReplayFolderPath  string
}

func (c *Config) EnsureConfigValid() {
	if c.LanguageCode == "" {
		c.LanguageCode = "zh"
	}
}

func GetConf() Config {
	if GlobalConfig == nil {
		return getDefaultConfig()
	}
	return *GlobalConfig
}

var GlobalConfig *Config

func Setup(configPath string) {
	var conf = getDefaultConfig()
	loadConfigFromEnv(&conf)
	loadConfigFromFile(configPath, &conf)
	conf.EnsureConfigValid()
	GlobalConfig = &conf
	log.Printf("%+v\n", GlobalConfig)
}

func getDefaultConfig() Config {
	defaultName := getDefaultName()
	rootPath := getPwdDirPath()
	dataFolderPath := filepath.Join(rootPath, "data")
	replayFolderPath := filepath.Join(dataFolderPath, "replays")
	LogDirPath := filepath.Join(dataFolderPath, "logs")
	keyFolderPath := filepath.Join(dataFolderPath, "keys")
	accessKeyFilePath := filepath.Join(keyFolderPath, ".access_key")

	folders := []string{dataFolderPath, replayFolderPath, keyFolderPath, LogDirPath}
	for i := range folders {
		if err := EnsureDirExist(folders[i]); err != nil {
			log.Fatalf("Create folder failed: %s", err)
		}
	}
	return Config{
		Name:              defaultName,
		CoreHost:          "http://localhost:8080",
		BootstrapToken:    "",
		BindHost:          "0.0.0.0",
		SSHPort:           "2222",
		SSHTimeout:        15,
		HTTPPort:          "5000",
		AccessKeyFilePath: accessKeyFilePath,
		LogLevel:          "INFO",
		RootPath:          rootPath,
		DataFolderPath:    dataFolderPath,
		LogDirPath:        LogDirPath,
		KeyFolderPath:     keyFolderPath,
		ReplayFolderPath:  replayFolderPath,

		Comment:             "KOKO",
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

		EnableLocalPortForward:   false,
		EnableReversePortForward: false,
		EnableIDESupport:         false,
	}

}

func EnsureDirExist(path string) error {
	if !haveDir(path) {
		if err := os.MkdirAll(path, os.ModePerm); err != nil {
			return err
		}
	}
	return nil
}

func have(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
func haveDir(file string) bool {
	fi, err := os.Stat(file)
	return err == nil && fi.IsDir()
}

func getPwdDirPath() string {
	if rootPath, err := os.Getwd(); err == nil {
		return rootPath
	}
	return ""
}

func loadConfigFromEnv(conf *Config) {
	viper.AutomaticEnv() // 全局配置，用于其他 pkg 包可以用 viper 获取环境变量的值
	envViper := viper.New()
	for _, item := range os.Environ() {
		envItem := strings.SplitN(item, "=", 2)
		if len(envItem) == 2 {
			envViper.Set(envItem[0], viper.Get(envItem[0]))
		}
	}
	if err := envViper.Unmarshal(conf); err == nil {
		log.Println("Load config from env")
	}
}

func loadConfigFromFile(path string, conf *Config) {
	var err error
	if have(path) {
		fileViper := viper.New()
		fileViper.SetConfigFile(path)
		if err = fileViper.ReadInConfig(); err == nil {
			if err = fileViper.Unmarshal(conf); err == nil {
				log.Printf("Load config from %s success\n", path)
				return
			}
		}
	}
	if err != nil {
		log.Fatalf("Load config from %s failed: %s\n", path, err)
	}
}

const (
	prefixName = "[KoKo]-"

	hostEnvKey = "SERVER_HOSTNAME"

	defaultNameMaxLen = 128
)

/*
SERVER_HOSTNAME: 环境变量名，可用于自定义默认注册名称的前缀
default name rule:
[Koko]-{SERVER_HOSTNAME}-{HOSTNAME}
 or
[Koko]-{HOSTNAME}
*/

func getDefaultName() string {
	hostname, _ := os.Hostname()
	if serverHostname, ok := os.LookupEnv(hostEnvKey); ok {
		hostname = fmt.Sprintf("%s-%s", serverHostname, hostname)
	}
	hostRune := []rune(prefixName + hostname)
	if len(hostRune) <= defaultNameMaxLen {
		return string(hostRune)
	}
	name := make([]rune, defaultNameMaxLen)
	index := defaultNameMaxLen / 2
	copy(name[:index], hostRune[:index])
	start := len(hostRune) - index
	copy(name[index:], hostRune[start:])
	return string(name)
}

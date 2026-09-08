package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"

	"github.com/jumpserver/koko/pkg/common"
)

var (
	CipherKey = "JumpServer Cipher Key for KoKo !"

	KubectlBanner = "Welcome to JumpServer kubectl, try kubectl --help."
)

type Config struct {
	Name                     string `mapstructure:"NAME"`
	CoreHost                 string `mapstructure:"CORE_HOST"`
	BootstrapToken           string `mapstructure:"BOOTSTRAP_TOKEN"`
	BindHost                 string `mapstructure:"BIND_HOST"`
	SSHPort                  string `mapstructure:"SSHD_PORT"`
	HTTPPort                 string `mapstructure:"HTTPD_PORT"`
	WebProxyEnabled          bool   `mapstructure:"WEB_PROXY_ENABLED"`
	WebProxyBindHost         string `mapstructure:"WEB_PROXY_BIND_HOST"`
	WebProxyPort             string `mapstructure:"WEB_PROXY_PORT"`
	WebProxyAllowedHosts     string `mapstructure:"WEB_PROXY_ALLOWED_HOSTS"`
	WebProxyRecordingEnabled bool   `mapstructure:"WEB_PROXY_RECORDING_ENABLED"`
	WebProxyFFmpegPath       string `mapstructure:"WEB_PROXY_FFMPEG_PATH"`
	SSHTimeout               int    `mapstructure:"SSH_TIMEOUT"`
	HttpRequestTimeout       int    `mapstructure:"HTTP_REQUEST_TIMEOUT"`

	LogLevel string `mapstructure:"LOG_LEVEL"`

	Comment             string `mapstructure:"COMMENT"`
	LanguageCode        string `mapstructure:"LANGUAGE_CODE"`
	UploadFailedReplay  bool   `mapstructure:"UPLOAD_FAILED_REPLAY_ON_START"`
	UploadFailedFTPFile bool   `mapstructure:"UPLOAD_FAILED_FTP_FILE_ON_START"`
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

	RedisSentinelPassword string `mapstructure:"REDIS_SENTINEL_PASSWORD"`
	RedisSentinelHosts    string `mapstructure:"REDIS_SENTINEL_HOSTS"`
	RedisUseSSL           bool   `mapstructure:"REDIS_USE_SSL"`

	GuacdAddrs string `mapstructure:"GUACD_ADDRS"`
	GuaHost    string `mapstructure:"GUA_HOST"`
	GuaPort    string `mapstructure:"GUA_PORT"`

	CleanDriveScheduleTime int `mapstructure:"JUMPSERVER_CLEAN_DRIVE_SCHEDULE_TIME"`

	EnableVideoWorker bool   `mapstructure:"ENABLE_VIDEO_WORKER"`
	VideoWorkerHost   string `mapstructure:"VIDEO_WORKER_HOST"`
	IgnoreVerifyCerts bool   `mapstructure:"IGNORE_VERIFY_CERTS"`
	PandaHost         string `mapstructure:"PANDA_HOST"`
	EnablePanda       bool   `mapstructure:"ENABLE_PANDA"`

	ReplayMaxSize         int    `mapstructure:"REPLAY_MAX_SIZE"`
	VncClipboardEncoding  string `mapstructure:"VNC_CLIPBOARD_ENCODING"`
	DisableKeyboardRecord bool   `mapstructure:"DISABLE_KEYBOARD_RECORD"`
	DriveScope            string `mapstructure:"LION_DRIVE_SCOPE"`

	EnableLocalPortForward bool `mapstructure:"ENABLE_LOCAL_PORT_FORWARD"`
	EnableVscodeSupport    bool `mapstructure:"ENABLE_VSCODE_SUPPORT"`

	EnableReversePortForward bool `mapstructure:"ENABLE_REVERSE_PORT_FORWARD"`

	KokoPrivileged bool `mapstructure:"KOKO_PRIVILEGED"`

	HiddenFields []string `mapstructure:"HIDDEN_FIELDS"`

	// 仅控制是否缓存 sftp 的 token 连接
	ConnectionTokenReusable bool `mapstructure:"CONNECTION_TOKEN_REUSABLE"`

	SshMaxSessions int `mapstructure:"SSH_MAX_SESSIONS"`

	DisableInputAsCommand bool `mapstructure:"DISABLE_INPUT_AS_COMMAND"`

	SecretEncryptKey string `mapstructure:"SECRET_ENCRYPT_KEY"`

	// Force both public key and password authentication (two-factor SSH login)
	ForceMultiAuth bool `mapstructure:"FORCE_MULTI_AUTH"`

	// DOMAINS=* "demo.example.com:443,172.17.200.191:80"
	DOMAINS string `mapstructure:"DOMAINS"`

	RootPath          string
	DataFolderPath    string
	LogDirPath        string
	KeyFolderPath     string
	AccessKeyFilePath string
	ReplayFolderPath  string
	FTPFileFolderPath string
	CertsFolderPath   string

	// Lion compatibility paths. RecordPath and FTPFilePath intentionally point
	// at Koko's existing replay and FTP directories.
	DrivePath         string
	RecordPath        string
	FTPFilePath       string
	SessionFolderPath string
}

func (c *Config) EnsureConfigValid() {
	if c.LanguageCode == "" {
		c.LanguageCode = "en"
	}
}

func (c *Config) UpdateRedisPassword(val string) {
	c.RedisPassword = val
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
	log.Printf("Config loaded: name=%q bind=%s ssh_port=%s http_port=%s share_room_type=%s\n",
		conf.Name, conf.BindHost, conf.SSHPort, conf.HTTPPort, conf.ShareRoomType)
}

func getDefaultConfig() Config {
	defaultName := getDefaultName()
	rootPath := getPwdDirPath()
	dataFolderPath := filepath.Join(rootPath, "data")
	replayFolderPath := filepath.Join(dataFolderPath, "replays")
	ftpFileFolderPath := filepath.Join(dataFolderPath, "ftp_files")
	driveFolderPath := filepath.Join(dataFolderPath, "drive")
	sessionFolderPath := filepath.Join(dataFolderPath, "sessions")
	LogDirPath := filepath.Join(dataFolderPath, "logs")
	keyFolderPath := filepath.Join(dataFolderPath, "keys")
	CertsFolderPath := filepath.Join(dataFolderPath, "certs")
	accessKeyFilePath := filepath.Join(keyFolderPath, ".access_key")

	folders := []string{dataFolderPath, replayFolderPath, driveFolderPath,
		sessionFolderPath, keyFolderPath, LogDirPath, CertsFolderPath}
	for i := range folders {
		if err := EnsureDirExist(folders[i]); err != nil {
			log.Fatalf("Create folder failed: %s", err)
		}
	}
	return Config{
		Name:                     defaultName,
		CoreHost:                 "http://localhost:8080",
		BootstrapToken:           "",
		BindHost:                 "0.0.0.0",
		SSHPort:                  "2222",
		SSHTimeout:               15,
		HttpRequestTimeout:       30,
		HTTPPort:                 "5000",
		WebProxyEnabled:          true,
		WebProxyBindHost:         "127.0.0.1",
		WebProxyPort:             "5001",
		WebProxyAllowedHosts:     "*",
		WebProxyRecordingEnabled: true,
		WebProxyFFmpegPath:       "ffmpeg",
		AccessKeyFilePath:        accessKeyFilePath,
		LogLevel:                 "INFO",
		RootPath:                 rootPath,
		DataFolderPath:           dataFolderPath,
		LogDirPath:               LogDirPath,
		KeyFolderPath:            keyFolderPath,
		ReplayFolderPath:         replayFolderPath,
		FTPFileFolderPath:        ftpFileFolderPath,
		CertsFolderPath:          CertsFolderPath,
		DrivePath:                driveFolderPath,
		RecordPath:               replayFolderPath,
		FTPFilePath:              ftpFileFolderPath,
		SessionFolderPath:        sessionFolderPath,
		LanguageCode:             "en",

		Comment:                "KOKO",
		UploadFailedReplay:     true,
		UploadFailedFTPFile:    true,
		ShowHiddenFile:         false,
		ReuseConnection:        true,
		AssetLoadPolicy:        "",
		ZipMaxSize:             "1024M",
		ZipTmpPath:             "/tmp",
		ClientAliveInterval:    30,
		RetryAliveCountMax:     3,
		ShareRoomType:          "local",
		RedisHost:              "127.0.0.1",
		RedisPort:              "6379",
		RedisPassword:          "",
		GuaHost:                "127.0.0.1",
		GuaPort:                "4822",
		CleanDriveScheduleTime: 1,
		PandaHost:              "http://panda:9001",
		VideoWorkerHost:        "http://video:9000",
		ReplayMaxSize:          defaultLionReplayMaxSize,
		DriveScope:             LionDriveScopeUser,

		EnableLocalPortForward: false,
		EnableVscodeSupport:    false,
		KokoPrivileged:         true,
		DisableInputAsCommand:  true,
	}

}

func EnsureDirExist(path string) error {
	if !haveDir(path) {
		if err := os.MkdirAll(path, 0700); err != nil {
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
[Koko]-{SERVER_HOSTNAME}-{HOSTNAME}-RandomStr
 or
[Koko]-{HOSTNAME}-RandomStr
*/

func getDefaultName() string {
	hostname, _ := os.Hostname()
	hostname = fmt.Sprintf("%s-%s", hostname, common.RandomStr(7))
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

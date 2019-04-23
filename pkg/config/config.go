package config

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

type Config struct {
	AssetListPageSize   string            `json:"TERMINAL_ASSET_LIST_PAGE_SIZE"`
	AssetListSortBy     string            `json:"TERMINAL_ASSET_LIST_SORT_BY"`
	HeaderTitle         string            `json:"TERMINAL_HEADER_TITLE"`
	HostKey             string            `json:"TERMINAL_HOST_KEY" yaml:"HOST_KEY"`
	PasswordAuth        bool              `json:"TERMINAL_PASSWORD_AUTH" yaml:"PASSWORD_AUTH"`
	PublicKeyAuth       bool              `json:"TERMINAL_PUBLIC_KEY_AUTH" yaml:"PUBLIC_KEY_AUTH"`
	CommandStorage      map[string]string `json:"TERMINAL_COMMAND_STORAGE"`
	ReplayStorage       map[string]string `json:"TERMINAL_REPLAY_STORAGE" yaml:"REPLAY_STORAGE"`
	SessionKeepDuration int               `json:"TERMINAL_SESSION_KEEP_DURATION"`
	TelnetRegex         string            `json:"TERMINAL_TELNET_REGEX"`
	MaxIdleTime         time.Duration     `json:"SECURITY_MAX_IDLE_TIME"`
	Name                string            `yaml:"NAME"`
	HostKeyFile         string            `yaml:"HOST_KEY_FILE"`
	CoreHost            string            `yaml:"CORE_HOST"`
	BootstrapToken      string            `yaml:"BOOTSTRAP_TOKEN"`
	BindHost            string            `yaml:"BIND_HOST"`
	SSHPort             int               `yaml:"SSHD_PORT"`
	HTTPPort            int               `yaml:"HTTPD_PORT"`
	AccessKey           string            `yaml:"ACCESS_KEY"`
	AccessKeyFile       string            `yaml:"ACCESS_KEY_FILE"`
	LogLevel            string            `yaml:"LOG_LEVEL"`
	HeartbeatDuration   time.Duration     `yaml:"HEARTBEAT_INTERVAL"`
	RootPath            string
	Comment             string

	mux sync.RWMutex
}

func (c *Config) EnsureConfigValid() {
}

func (c *Config) LoadFromYAML(body []byte) error {
	c.mux.Lock()
	defer c.mux.Unlock()
	err := yaml.Unmarshal(body, c)
	if err != nil {
		log.Errorf("Load yaml error: %v", err)
	}
	return err
}

func (c *Config) LoadFromYAMLPath(filepath string) error {
	body, err := ioutil.ReadFile(filepath)
	if err != nil {
		log.Errorf("Not found file: %s", filepath)
		os.Exit(1)
	}
	return c.LoadFromYAML(body)
}

func (c *Config) LoadFromJSON(body []byte) error {
	c.mux.Lock()
	defer c.mux.Unlock()
	err := json.Unmarshal(body, c)
	if err != nil {
		fmt.Println("Load yaml err")
		os.Exit(1)
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
		log.Errorf("Error occur: %v", err)
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

var name, _ = os.Hostname()
var rootPath, _ = os.Getwd()
var Conf = &Config{
	Name:           name,
	CoreHost:       "http://localhost:8080",
	BootstrapToken: "",
	BindHost:       "0.0.0.0",
	SSHPort:        2222,
	HTTPPort:       5000,
	AccessKey:      "",
	AccessKeyFile:  "access_key",
	LogLevel:       "DEBUG",
	HostKeyFile:    "host_key",
	HostKey:        "",
	RootPath:       rootPath,
	Comment:        "Coco",
	ReplayStorage:  map[string]string{},
	CommandStorage: map[string]string{},
}

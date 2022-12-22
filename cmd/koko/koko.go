package main

import (
	"flag"
	"fmt"
	"time"

	"github.com/jumpserver/koko/pkg/koko"
)

var (
	Buildstamp = ""
	Githash    = ""
	Goversion  = ""
	Version    = "unknown"

	infoFlag = false

	configPath = ""
)

func init() {
	flag.StringVar(&configPath, "f", "config.yml", "config.yml path")
	flag.BoolVar(&infoFlag, "V", false, "version info")
}

func main() {
	flag.Parse()
	if infoFlag {
		fmt.Printf("Version:             %s\n", Version)
		fmt.Printf("Git Commit Hash:     %s\n", Githash)
		fmt.Printf("UTC Build Time :     %s\n", Buildstamp)
		fmt.Printf("Go Version:          %s\n", Goversion)
		return
	}
	fmt.Printf(startWelcomeMsg, time.Now().Format(timeFormat), Version)
	koko.RunForever(configPath)
}

const (
	timeFormat      = "2006-01-02 15:04:05"
	startWelcomeMsg = `%s
KoKo Version %s, more see https://www.jumpserver.org
Quit the server with CONTROL-C.
`
)

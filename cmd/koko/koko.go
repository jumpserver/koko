package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jumpserver/koko/pkg/chen"
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
	if len(os.Args) > 1 && os.Args[1] == "chen" {
		if err := chen.RunCommand(os.Args[2:], Version, os.Stdout); err != nil && !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
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
KoKo Version %s, more see https://www.jumpserver.com
Quit the server with CONTROL-C.
`
)

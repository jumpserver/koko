package main

import (
	"flag"
	"fmt"

	"github.com/jumpserver/koko/pkg/koko"
)

var (
	Buildstamp = ""
	Githash    = ""
	Goversion  = ""

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
		fmt.Printf("Version:             %s\n", koko.Version)
		fmt.Printf("Git Commit Hash:     %s\n", Githash)
		fmt.Printf("UTC Build Time :     %s\n", Buildstamp)
		fmt.Printf("Go Version:          %s\n", Goversion)
		return
	}
	koko.RunForever(configPath)
}

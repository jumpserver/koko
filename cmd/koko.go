package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strconv"
	"syscall"

	"github.com/sevlyar/go-daemon"

	"github.com/jumpserver/koko/pkg/koko"
)

func startAsDaemon() {
	ctx := &daemon.Context{
		PidFileName: "/tmp/koko.pid",
		PidFilePerm: 0644,
		Umask:       027,
		WorkDir:     "./",
	}
	child, err := ctx.Reborn()
	if err != nil {
		log.Fatalf("run failed: %v", err)
	}
	if child != nil {
		return
	}
	defer ctx.Release()
	koko.RunForever()
}

var (
	Buildstamp = ""
	Githash    = ""
	Goversion  = ""

	pidPath = "/tmp/koko.pid"

	daemonFlag    = false
	runSignalFlag = "start"
	infoFlag      = false
)

func init() {
	flag.BoolVar(&daemonFlag, "d", false, "start as Daemon")
	flag.StringVar(&runSignalFlag, "s", "start", "start | stop")
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

	if runSignalFlag == "stop" {
		pid, err := ioutil.ReadFile(pidPath)
		if err != nil {
			log.Fatal("File not exist")
			return
		}
		pidInt, _ := strconv.Atoi(string(pid))
		err = syscall.Kill(pidInt, syscall.SIGTERM)
		if err != nil {
			log.Fatalf("Stop failed: %v", err)
		} else {
			_ = os.Remove(pidPath)
		}
		return
	}

	switch {
	case daemonFlag:
		startAsDaemon()
	default:
		koko.RunForever()
	}
}
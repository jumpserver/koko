package koko

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/httpd"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/service"
	"github.com/jumpserver/koko/pkg/sshd"
)

const version = "1.5.0"

type Coco struct {
}

func (c *Coco) Start() {
	fmt.Println(time.Now().Format("2006-01-02 15:04:05"))
	fmt.Printf("Coco version %s, more see https://www.jumpserver.org\n", version)
	fmt.Println("Quit the server with CONTROL-C.")
	go sshd.StartServer()
	go httpd.StartHTTPServer()
}

func (c *Coco) Stop() {
	sshd.StopServer()
	httpd.StopHTTPServer()
	logger.Debug("Quit The Coco")
}

func RunForever() {
	bootstrap()
	gracefulStop := make(chan os.Signal)
	signal.Notify(gracefulStop, syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT)
	app := &Coco{}
	app.Start()
	<-gracefulStop
	app.Stop()
}

func bootstrap() {
	config.Initial()
	logger.Initial()
	service.Initial()
	Initial()
}

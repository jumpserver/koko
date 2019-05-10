package coco

import (
	"fmt"
	"time"

	"cocogo/pkg/sshd"
)

const version = "1.4.0"

type Coco struct {
}

func (c *Coco) Start() {
	fmt.Println(time.Now().Format("2006-01-02 15:04:05"))
	fmt.Printf("Coco version %s, more see https://www.jumpserver.org\n", version)
	fmt.Println("Quit the server with CONTROL-C.")
	sshd.StartServer()
}

func (c *Coco) Stop() {

}

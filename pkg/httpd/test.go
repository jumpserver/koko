package httpd

import (
	"fmt"
	"github.com/googollee/go-socket.io"
	"github.com/satori/go.uuid"
)

func TestOnConnectHandler(s socketio.Conn) error {
	s.SetContext("")
	fmt.Println("connected:", s.ID())
	return nil
}

func TestOnHostHandler(s socketio.Conn, msg HostMsg) {
	fmt.Println("On host")
	secret := msg.Secret
	clientID := uuid.NewV4().String()
	emitMsg := EmitRoomMsg{clientID, secret}
	s.Emit("room", emitMsg)
	s.Emit("data", DataMsg{Room: clientID, Data: "Hello world"})
}

func TestOnDataHandler(s socketio.Conn, msg string) {
	s.Emit("data", msg)
}

func TestOnResizeHandler(s socketio.Conn, msg ResizeMsg) {
	fmt.Println("On Resize msg")
}

func TestOnLogoutHandler(s socketio.Conn, msg string) {
	fmt.Println("On logout msg")
}

package httpd

import (
	"net/http"
	"time"

	gorillaws "github.com/gorilla/websocket"

	"github.com/kataras/neffos"
	"github.com/kataras/neffos/gorilla"

	"github.com/jumpserver/koko/pkg/logger"
)

var upgrader = gorilla.Upgrader(gorillaws.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
})

var wsEvents = neffos.WithTimeout{
	ReadTimeout:  Timeout * time.Second,
	WriteTimeout: Timeout * time.Second,
	Namespaces: neffos.Namespaces{
		"ssh": neffos.Events{
			neffos.OnNamespaceConnected:  OnNamespaceConnected,
			neffos.OnNamespaceDisconnect: OnNamespaceDisconnect,
			neffos.OnRoomJoined: func(c *neffos.NSConn, msg neffos.Message) error {
				return nil
			},
			neffos.OnRoomLeft: func(c *neffos.NSConn, msg neffos.Message) error {
				return nil
			},

			"data":   OnDataHandler,
			"resize": OnResizeHandler,
			"host":   OnHostHandler,
			"logout": OnLogoutHandler,
			"token":  OnTokenHandler,
			"ping":   OnPingHandler,

			"shareRoom": OnShareRoom,
		},
		"elfinder": neffos.Events{
			neffos.OnNamespaceConnected:  OnELFinderConnect,
			neffos.OnNamespaceDisconnect: OnELFinderDisconnect,
			"ping":                       OnPingHandler,
		},
	},
}

func neffosOnUpgradeError(err error) {
	if ok := neffos.IsTryingToReconnect(err); ok {
		logger.Debugf("A client was tried to reconnect err: %s", err)
		return
	}
	logger.Errorf("Upgrade Error: %s", err)
}

func neffosOnConnect(c *neffos.Conn) error {
	if c.WasReconnected() {
		logger.Debugf("ws %s reconnected, with tries: %d", c.ID(), c.ReconnectTries)
	} else {
		logger.Debugf("A new ws %s arrive", c.ID())
	}
	return nil
}

func neffosOnDisconnect(c *neffos.Conn) {
	logger.Debugf("Ws %s connection disconnect", c.ID())
	if conn, ok := websocketManager.GetUserCon(c.ID()); ok {
		conn.Close()
		websocketManager.DeleteUserCon(c.ID())
		logger.Infof("User %s ws %s disconnect.", conn.User.Name, c.ID())
	}
}

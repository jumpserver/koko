package koko

import (
	"github.com/jumpserver/koko/pkg/httpd"
	"github.com/kataras/neffos"
)

func (a *Application) neffosOnUpgradeError(err error) {
	httpd.NeffosOnUpgradeError(err)
}

func (a *Application) neffosOnConnect(c *neffos.Conn) error {
	return httpd.NeffosOnConnect(c)
}

func (a *Application) neffosOnDisconnect(c *neffos.Conn) {
	httpd.NeffosOnDisconnect(c)
}

func (a *Application) OnNamespaceConnected(ns *neffos.NSConn, msg neffos.Message) error {
	return httpd.OnNamespaceConnected(ns, msg)
}

func (a *Application) OnNamespaceDisconnect(ns *neffos.NSConn, msg neffos.Message) error {
	return httpd.OnNamespaceDisconnect(ns, msg)
}

func (a *Application) OnDataHandler(ns *neffos.NSConn, msg neffos.Message) error {
	return httpd.OnDataHandler(ns, msg)
}

func (a *Application) OnHostHandler(ns *neffos.NSConn, msg neffos.Message) error {
	return httpd.OnHostHandler(ns, msg)
}

func (a *Application) OnResizeHandler(ns *neffos.NSConn, msg neffos.Message) error {
	return httpd.OnResizeHandler(ns, msg)
}

func (a *Application) OnLogoutHandler(ns *neffos.NSConn, msg neffos.Message) error {
	return httpd.OnLogoutHandler(ns, msg)
}

func (a *Application) OnTokenHandler(ns *neffos.NSConn, msg neffos.Message) error {
	return httpd.OnTokenHandler(ns, msg)
}

func (a *Application) OnPingHandler(ns *neffos.NSConn, msg neffos.Message) error {
	return httpd.OnPingHandler(ns, msg)
}

func (a *Application) OnShareRoom(ns *neffos.NSConn, msg neffos.Message) error {
	return httpd.OnShareRoom(ns, msg)
}

func (a *Application) OnELFinderConnect(ns *neffos.NSConn, msg neffos.Message) error {
	return httpd.OnELFinderConnect(ns, msg)
}

func (a *Application) OnELFinderDisconnect(ns *neffos.NSConn, msg neffos.Message) error {
	return httpd.OnELFinderDisconnect(ns, msg)
}

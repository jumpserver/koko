package httpd

/*
func OnELFinderConnect(ns *neffos.NSConn, msg neffos.Message) error {
	logger.Debugf("Web folder ws %s connect", ns.Conn.ID())
	userConn, err := NewUserWebsocketConnWithSession(ns)
	if err != nil {
		logger.Errorf("Web folder ws %s connect err: %s", ns.Conn.ID(), err)
		ns.Emit("data", neffos.Marshal(err.Error()))
		ns.Emit("disconnect", []byte(""))
		ns.Conn.Close()
		return err
	}
	data := EmitSidMsg{Sid: ns.Conn.ID()}
	ns.Emit("data", neffos.Marshal(data))
	logger.Infof("Accepted user %s connect elfinder ws", userConn.User.Username)
	websocketManager.AddUserCon(ns.Conn.ID(), userConn)
	go userConn.loopHandler()
	return nil
}

func OnELFinderDisconnect(c *neffos.NSConn, msg neffos.Message) error {
	logger.Infof("Web folder ws %s disconnect", c.Conn.ID())
	removeUserVolume(c.Conn.ID())
	return nil
}

*/

package httpd

import (
	"html/template"
	"log"
	"net/http"

	"github.com/LeeEirc/elfinder"
	socketio "github.com/googollee/go-socket.io"
	"github.com/gorilla/mux"
)

func OnELFinderConnect(s socketio.Conn) error {
	u := s.URL()
	sid := u.Query().Get("sid")
	data := EmitSidMsg{Sid: sid}
	s.Emit("data", data)
	return nil
}

func OnELFinderDisconnect(s socketio.Conn, msg string) {
	u := s.URL()
	sid := u.Query().Get("sid")
	log.Println("disconnect: ", sid)
	log.Println("disconnect msg ", msg)
}

func sftpHostFinder(wr http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	tmpl := template.Must(template.ParseFiles("./templates/elfinder/file_manager.html"))
	hostID := vars["host"]
	_ = tmpl.Execute(wr, hostID)
}

func sftpFinder(wr http.ResponseWriter, req *http.Request) {
	tmpl := template.Must(template.ParseFiles("./templates/elfinder/file_manager.html"))
	_ = tmpl.Execute(wr, "_")
}

func sftpHostConnectorView(wr http.ResponseWriter, req *http.Request) {
	con := elfinder.NewElFinderConnector([]elfinder.Volume{&elfinder.DefaultVolume})
	con.ServeHTTP(wr, req)
}

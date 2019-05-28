package main

import (
	"fmt"
	"log"
	"net/http"

	socketio "github.com/mlsquires/socketio"
)

func main() {
	server, err := socketio.NewServer(nil)
	if err != nil {
		log.Fatal(err)
	}
	server.On("connection", func(s socketio.Socket) error {
		fmt.Println("connected:")
		return nil
	})
	server.On("/ssh", "host", func(s socketio.Conn, msg interface{}) {
		fmt.Println("host:")
	})
	server.On("/ssh", "data", func(s socketio.Conn, msg interface{}) {
		fmt.Println("On data")
	})
	server.OnEvent("/", "logout", func(s socketio.Conn) {
		fmt.Println("logout: ")
		last := s.Context().(string)
		s.Emit("bye", last)
	})
	server.OnError("/ssh", func(e error) {
		fmt.Println("meet error:", e)
	})
	server.OnDisconnect("/ssh", func(s socketio.Conn, msg string) {
		fmt.Println("closed", msg)
	})
	go server.Serve()
	defer server.Close()

	http.Handle("/socket.io/", server)
	http.Handle("/", http.FileServer(http.Dir("./asset")))
	log.Println("Serving at localhost:5000...")
	log.Fatal(http.ListenAndServe(":5000", nil))
}

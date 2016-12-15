// +build ignore

package main

import (
	"log"
	"net/http"
	"restwebsocket"
)

func handler(rw http.ResponseWriter, req *http.Request) {
	log.Println("writing response")
	rw.Write([]byte("Hello World!"))
}
func main() {
	wsServer := restwebsocket.NewWebSocketServer(":9090", 100, false, nil, nil, nil)

	ch, err := wsServer.Accept()
	if err != nil {
		log.Println("accept: ", err)
	}

	for conn := range ch {
		log.Println("new client connection")
		server := restwebsocket.NewRestServer(conn, http.HandlerFunc(handler))
		server.Serve()
	}
}

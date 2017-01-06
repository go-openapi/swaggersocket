// +build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"restwebsocket"
	"time"
)

func handler(rw http.ResponseWriter, req *http.Request) {
	rw.Write([]byte("Hello, Dolores!"))
}

func chunkedHandler(rw http.ResponseWriter, req *http.Request) {
	flusher, ok := rw.(http.Flusher)
	if !ok {
		panic("expected http.ResponseWriter to be an http.Flusher")
	}
	for i := 1; i <= 100; i++ {
		notify := rw.(http.CloseNotifier).CloseNotify()
		select {
		case <-notify:
			log.Println("connection closed...exiting handler")
			return
		default:
			fmt.Fprintf(rw, "Chunk #%d\n", i)
			flusher.Flush() // Trigger "chunked" encoding and send a chunk...
			time.Sleep(1 * time.Second)
		}
	}
}

func main() {
	wsServer := restwebsocket.NewWebSocketServer(":9090", 100, true, nil, nil, nil)

	ch, err := wsServer.Accept()
	if err != nil {
		log.Println("accept: ", err)
	}
	for conn := range ch {
		log.Println("new client connection")
		conn.Serve(context.Background(), http.HandlerFunc(chunkedHandler))
	}
}

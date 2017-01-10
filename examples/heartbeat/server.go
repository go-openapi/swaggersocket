// +build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"restwebsocket"
	"runtime"
	"time"
)

func simpleHandler(rw http.ResponseWriter, req *http.Request) {
	rw.Write([]byte("Hello, Dolores!"))
}

func chunkedHandler(rw http.ResponseWriter, req *http.Request) {
	flusher, ok := rw.(http.Flusher)
	if !ok {
		panic("expected http.ResponseWriter to be an http.Flusher")
	}
	for i := 1; i <= 10; i++ {
		fmt.Fprintf(rw, "Chunk #%d\n", i)
		flusher.Flush() // Trigger "chunked" encoding and send a chunk...
		time.Sleep(1 * time.Second)
	}
}

func closeNotifiedChunkedHandler(rw http.ResponseWriter, req *http.Request) {
	flusher, ok := rw.(http.Flusher)
	if !ok {
		panic("expected http.ResponseWriter to be an http.Flusher")
	}
	notify := rw.(http.CloseNotifier).CloseNotify()
	for i := 1; i <= 10; i++ {

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
	log.Printf("in main: #go-routines %v", runtime.NumGoroutine())
	for conn := range ch {
		log.Println("new client connection")
		//log.Printf("before serving: #go-routines %v", runtime.NumGoroutine())
		//conn.Serve runs in its own go-routine
		conn.Serve(context.Background(), http.HandlerFunc(closeNotifiedChunkedHandler))
		//time.Sleep(5 * time.Second)
		//conn.Close()
		//log.Printf("after serving: #go-routines %v", runtime.NumGoroutine())
	}
}

// +build ignore

package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"restwebsocket"
)

func echoHandler(rw http.ResponseWriter, req *http.Request) {
	p := req.URL.Path
	rw.Write([]byte(path.Base(p)))
}

func main() {
	u, _ := url.Parse("ws://localhost:9090/")
	for i := 0; i < 30; i++ {
		socketclient := restwebsocket.NewWebSocketClient(u, true, nil, nil, nil).WithMetaData(fmt.Sprintf("Client%d", i))
		if err := socketclient.Connect(); err != nil {
			panic(err)
		}
		socketclient.Connection().Serve(context.Background(), http.HandlerFunc(echoHandler))
	}
	ch := make(chan bool)
	<-ch
}

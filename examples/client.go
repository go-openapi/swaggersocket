package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"restwebsocket"
)

func main() {
	wsClient := restwebsocket.NewWebSocketClient(false, nil, nil, nil)
	u := &url.URL{
		Host:   "localhost:9090",
		Scheme: "ws",
	}
	if err := wsClient.Connect(u); err != nil {
		log.Println("connect: ", err)
	}

	done := make(chan struct{})

	reqPath := &url.URL{
		Path: "/karim",
	}
	req := &http.Request{
		URL:  reqPath,
		Body: ioutil.NopCloser(bytes.NewBuffer(nil)),
	}
	wsClient.Connection().WriteRequest(req)
	resp := wsClient.Connection().ReadResponse()
	fmt.Printf("%v", resp.Body)

	<-done
}

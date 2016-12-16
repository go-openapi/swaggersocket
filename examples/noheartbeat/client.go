package main

import (
	"bytes"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"restwebsocket"
	"time"
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
		Path: "/",
	}
	req := &http.Request{
		URL:  reqPath,
		Body: ioutil.NopCloser(bytes.NewBuffer(nil)),
	}
	for i := 0; i < 5; i++ {
		wsClient.Connection().WriteRequest(req)
		resp, _ := wsClient.Connection().ReadResponse()
		buf, _ := ioutil.ReadAll(resp.Body)
		log.Println("response: ", string(buf))
		time.Sleep(time.Second * 90)
	}

	<-done
}

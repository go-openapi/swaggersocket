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
	wsClient := restwebsocket.NewWebSocketClient(true, nil, nil, []byte("ping"))
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
	for i := 0; i < 5; i++ {
		wsClient.Connection().WriteRequest(req)
		resp := wsClient.Connection().ReadResponse()
		buf, _ := ioutil.ReadAll(resp.Body)
		log.Println("response: ", string(buf))
		time.Sleep(time.Second * 90)
	}

	<-done
}

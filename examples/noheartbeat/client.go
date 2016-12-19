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

	u := &url.URL{
		Host:   "localhost:9090",
		Scheme: "ws",
	}
	wsClient := restwebsocket.NewWebSocketClient(u, false, nil, nil, nil)
	if err := wsClient.Connect(); err != nil {
		log.Println("connect: ", err)
	}

	reqPath := &url.URL{
		Path: "/",
	}
	req := &http.Request{
		URL:  reqPath,
		Body: ioutil.NopCloser(bytes.NewBuffer(nil)),
	}
	for i := 0; i < 4; i++ {
		err := wsClient.Connection().WriteRequest(req)
		if err != nil {
			log.Println("err: ", err)
			continue
		}
		resp, err := wsClient.Connection().ReadResponse()
		if err != nil {
			continue
		}
		buf, _ := ioutil.ReadAll(resp.Body)
		log.Println("response: ", string(buf))
		resp.Body.Close()
		time.Sleep(time.Second * 2)
	}
}

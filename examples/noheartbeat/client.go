package main

import (
	"bufio"
	"bytes"
	"io"
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
		log.Printf("Headers: %+v", resp.Header)
		log.Printf("TE: %v\n", resp.TransferEncoding)
		if err != nil {
			continue
		}
		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadBytes('\n')
			log.Println(string(line))
			if err == io.EOF {
				log.Println("EOF reached")
				break
			}
			if err != nil {
				log.Printf("xxx: %v", err)
				break
			}
		}
		resp.Body.Close()
		time.Sleep(time.Second * 1)
	}
}

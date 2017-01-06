package main

import (
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
	wsClient := restwebsocket.NewWebSocketClient(u, true, nil, nil, nil)
	if err := wsClient.Connect(); err != nil {
		log.Println("connect: ", err)
	}
	//defer wsClient.Connection().Close()

	reqPath := &url.URL{
		Path: "/",
	}
	req := &http.Request{
		URL:  reqPath,
		Body: ioutil.NopCloser(bytes.NewBuffer(nil)),
	}
	for i := 0; i < 50; i++ {
		err := wsClient.Connection().WriteRequest(req)
		if err != nil {
			log.Println("err: ", err)
			time.Sleep(time.Second * 1)
			continue
		}
		resp, err := wsClient.Connection().ReadResponse()
		if err != nil {
			time.Sleep(time.Second * 1)
			continue
		}
		//reader := bufio.NewReader(resp.Body)
		readbuf := make([]byte, 4096)
		for {
			//line, err := reader.ReadBytes('\n')
			n, err := resp.Body.Read(readbuf)
			if n > 0 {
				log.Printf("%d bytes read, body is: %s", n, string(readbuf))
			}
			if err == io.EOF {
				log.Println("EOF reached")
				break
			}
			if err != nil {
				log.Printf("error: %v", err)
				break
			}
		}
		resp.Body.Close()
		time.Sleep(time.Second * 1)
	}
}

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
	wsClient := restwebsocket.NewWebSocketClient(u, false, nil, nil, nil)
	if err := wsClient.Connect(); err != nil {
		log.Println("connect: ", err)
	}
	defer wsClient.Connection().Close()

	reqPath := &url.URL{
		Path: "/",
	}
	req := &http.Request{
		URL:  reqPath,
		Body: ioutil.NopCloser(bytes.NewBuffer(nil)),
	}
	for i := 0; i < 2; i++ {
		err := wsClient.Connection().WriteRequest(req)
		if err != nil {
			log.Println("err: ", err)
			continue
		}
		resp, err := wsClient.Connection().ReadResponse()
		log.Printf("Headers: %+v", resp.Header)
		log.Printf("Transfer-Encoding: %v\n", resp.TransferEncoding)
		if err != nil {
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
				// handle error
			}
		}
		resp.Body.Close()
		time.Sleep(time.Second * 1)
	}
}

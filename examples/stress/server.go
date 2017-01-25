package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/casualjim/swaggersocket"

	uuid "github.com/satori/go.uuid"
)

var (
	// The clientMap connects a clientName to a connection ID
	clientMap map[string]string
)

func main() {
	wsServer := swaggersocket.NewWebSocketServer(":9090", 100, true, nil, nil, nil)
	ch, err := wsServer.EventStream()
	clientMap = make(map[string]string)
	if err != nil {
		log.Println("accept: ", err)
	}
	for {
		select {
		case evt := <-ch:
			log.Printf("connection event received: %s", evt.EventType.String())
			cid := evt.ConnectionId
			if evt.EventType == swaggersocket.ConnectionReceived {
				clientMap[cid] = wsServer.MetaData(cid).(string)
			} else { // connection closed or connection failure
				delete(clientMap, cid)
			}
		default:
			log.Printf("length of clientMap is %d", len(clientMap))
			for _, clientID := range clientMap {
				c, err := wsServer.ConnectionFromMetaData(clientID)
				if c == "" {
					continue
				}
				if err != nil {
					log.Println(err)
					continue
				}
				cid := uuid.NewV4().String()
				req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("ws://localhost:9090/echo/%s/", cid), nil)
				req.Header.Set("X-Correlation-Id", cid)
				if err := wsServer.ConnectionFromID(c).WriteRequest(req); err != nil {
					log.Println(err)
					continue
				}
				if resp, err := wsServer.ConnectionFromID(c).ReadResponse(); err == nil {
					b, _ := ioutil.ReadAll(resp.Body)
					defer resp.Body.Close()
					if string(b) != cid || resp.Header.Get("X-Correlation-Id") != cid {
						panic("error response recieved")
					}
				}
			}
		}

	}
}


// Copyright 2015 go-swagger maintainers
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package swaggersocket

import (
	"math/rand"
	"time"

	"github.com/gorilla/websocket"
)

const (
	heartbeatPeriod = 5 * time.Second
	pingwriteWait   = 2 * time.Second
)

// The reason for heartbeat is to to utilize the underlying websocket ping pong mechanism to keep connections alive
type heartbeat struct {
	sockconn      *SocketConnection
	period        time.Duration
	pingWriteWait time.Duration
	pingMsg       []byte
	stopCh        chan chan struct{}
}

func newHeartBeat(c *SocketConnection, period, pingwritewait time.Duration, appData []byte) *heartbeat {
	r := 1 + rand.Intn(5)
	return &heartbeat{
		sockconn:      c,
		period:        time.Duration(r) * time.Second,
		pingWriteWait: pingwritewait,
		pingMsg:       appData,
		stopCh:        make(chan chan struct{}),
	}
}

func (hb *heartbeat) start() {

	go func() {
		// periodically send a ping control message
		ticker := time.NewTicker(hb.period)
		defer ticker.Stop()
		for {
			select {
			// extra caution is required when both channels have values AT THE SAME TIME
			case <-ticker.C:
				// write a websocket ping control message. WriteControl is safe to use concurrently
				if err := hb.sockconn.conn.WriteControl(websocket.PingMessage, hb.pingMsg, time.Now().Add(hb.pingWriteWait)); err != nil {
					if err != websocket.ErrCloseSent {
						if hb.sockconn.connType == ServerSide {
							hb.sockconn.log.Printf("heartbeat: connection failure detected at socketserver side")
						} else {
							hb.sockconn.log.Printf("heartbeat: connection failure detected at socketclient side")
						}

						// this has to be its own goroutine
						go hb.sockconn.handleFailure()
					}

				}
			case ch := <-hb.stopCh:
				ch <- struct{}{}
				hb.sockconn.log.Println("stopping heartbeat for connection")
				return
			}
		}
	}()
}

func (hb *heartbeat) stop() {
	ch := make(chan struct{})
	defer close(ch)
	hb.stopCh <- ch
	// this gurantees that stop() will not return unless the heartbeat actually stops
	// solves a race issue
	<-ch
	close(hb.stopCh)
}

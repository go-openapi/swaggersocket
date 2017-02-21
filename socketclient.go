
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
	"errors"
	"log"
	"net/url"
	"sync"

	"os"

	"github.com/cenkalti/backoff"
	"github.com/gorilla/websocket"
)

// WebsocketClient is the websockets client
type WebsocketClient struct {
	connMutex       sync.Mutex
	conn            *SocketConnection
	addr            *url.URL
	handlerLoop     func()
	isRestapiServer bool
	keepAlive       bool
	pingHdlr        func(string) error
	pongHdlr        func(string) error
	appData         []byte
	meta            interface{}
	log             Logger
}

// SocketClientOpts is the options for a socket client
type SocketClientOpts struct {
	URL       *url.URL
	KeepAlive bool
	PingHdlr  func(string) error
	PongHdlr  func(string) error
	AppData   []byte
	Logger    Logger
}

// NewWebSocketClient creates a new websocketcient
func NewWebSocketClient(opts SocketClientOpts) *WebsocketClient {
	cli := &WebsocketClient{
		keepAlive: opts.KeepAlive,
		pingHdlr:  opts.PingHdlr,
		pongHdlr:  opts.PongHdlr,
		appData:   opts.AppData,
		addr:      opts.URL,
		log:       opts.Logger,
	}

	if cli.log == nil {
		cli.log = log.New(os.Stdout, "", 0)
	}
	return cli
}

// WithMetaData associates the socket client with app-level metadata
func (sc *WebsocketClient) WithMetaData(meta interface{}) *WebsocketClient {
	sc.meta = meta
	return sc
}

// TODO refactor connect to return the connection instead of just an error. Creating a connection as a side effect is a bad idea
// Connect connects the websocket client to a server
func (sc *WebsocketClient) Connect() error {
	operation := func() error {
		conn, _, err := websocket.DefaultDialer.Dial(sc.addr.String(), nil)
		if err != nil {
			sc.log.Println("dial:", err)
			return err
		}
		// Connection established. Start Handshake
		connectionID, err := sc.startClientHandshake(conn, sc.meta)
		if err != nil {
			// handle error and close the connection
			panic(err)
		}

		opts := connectionOpts{
			Conn:        conn,
			ID:          connectionID,
			KeepAlive:   sc.keepAlive,
			PingHandler: sc.pingHdlr,
			PongHandler: sc.pongHdlr,
			AppData:     sc.appData,
			Logger:      sc.log,
		}

		c := NewSocketConnection(opts)
		c.setSocketClient(sc)
		c.setType(ClientSide)
		sc.connMutex.Lock()
		sc.conn = c
		sc.connMutex.Unlock()
		// start the heartbeat protocol
		if c.heartBeat != nil {
			c.heartBeat.start()
		}
		return nil
	}

	err := backoff.Retry(operation, backoff.NewExponentialBackOff())
	if err != nil {
		sc.log.Println("cannot initiate connection")
		return err
	}

	return nil
}

func (sc *WebsocketClient) startClientHandshake(c *websocket.Conn, meta interface{}) (string, error) {
	if err := c.WriteJSON(&clientHandshakeMetaData{
		Type: clientHandshakeMetaDataFrame,
		Meta: meta,
	}); err != nil {
		return "", err
	}
	connIDFrame := &serverHandshakeConnectionID{}
	if err := c.ReadJSON(connIDFrame); err != nil {
		return "", err
	}
	if connIDFrame.Type != serverHandshakeConnectionIDFrame {
		return "", errors.New("handshake error")
	}
	connID := connIDFrame.ConnectionID
	if err := c.WriteJSON(&clientHandshakeAck{
		Type: clientHandshakeAckFrame,
	}); err != nil {
		return "", err
	}
	return connID, nil
}

// Connection returns the socketConnection object
func (sc *WebsocketClient) Connection() *SocketConnection {
	sc.connMutex.Lock()
	c := sc.conn
	sc.connMutex.Unlock()
	return c
}


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
	"net/http"
	"sync"
	"time"

	"log"

	"os"

	"github.com/gorilla/websocket"
	"github.com/satori/go.uuid"
)

const (
	handshakeTimeout = 60 * time.Second
)

type EvtType int

const (
	ConnectionReceived EvtType = iota
	ConnectionClosed
	ConnectionFailure
)

func (e EvtType) String() string {
	if e == ConnectionReceived {
		return "ConnectionReceived"
	} else if e == ConnectionClosed {
		return "ConnectionClosed"
	}
	return "ConnectionFailure"
}

type ConnectionEvent struct {
	EventType    EvtType
	ConnectionId string
}

type WebsocketServer struct {
	upgrader           websocket.Upgrader
	addr               string
	connMapLock        sync.Mutex
	connectionMap      map[string]*SocketConnection
	connMetaLock       sync.Mutex
	connectionMetaData map[string]interface{}
	keepAlive          bool
	pingHdlr, pongHdlr func(string) error
	appData            []byte
	handlerLoop        func()
	isRestapiServer    bool
	apiHdlr            http.Handler
	eventStream        chan ConnectionEvent
	hasSubscriber      atomicBool
	register           chan *SocketConnection
	unregister         chan *SocketConnection
	log                Logger
}

// SocketServerOpts is the options for creating a websocket server
type SocketServerOpts struct {
	Addr      string
	KeepAlive bool
	PingHdlr  func(string) error
	PongHdlr  func(string) error
	AppData   []byte
	Log       Logger
}

// NewWebSocketServer creates a new websocket server
func NewWebSocketServer(opts SocketServerOpts) *WebsocketServer {
	srvr := &WebsocketServer{
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
		addr:               opts.Addr,
		keepAlive:          opts.KeepAlive,
		pingHdlr:           opts.PingHdlr,
		pongHdlr:           opts.PongHdlr,
		appData:            opts.AppData,
		connectionMap:      make(map[string]*SocketConnection),
		connectionMetaData: make(map[string]interface{}),
		register:           make(chan *SocketConnection),
		unregister:         make(chan *SocketConnection),
		eventStream:        make(chan ConnectionEvent),
		log:                opts.Log,
	}
	if srvr.log == nil {
		srvr.log = log.New(os.Stdout, "", 0)
	}
	serveMux := http.NewServeMux()
	serveMux.HandleFunc("/", srvr.websocketHandler)
	go http.ListenAndServe(srvr.addr, serveMux)
	return srvr
}

// RemoteAddr returns the remote address
func (ss *WebsocketServer) RemoteAddr(connID string) string {
	if netAddr := ss.ConnectionFromID(connID).remoteAddr(); netAddr != nil {
		return netAddr.String()
	}
	return ""
}

// MetaData reads the metadata for some connection
func (ss *WebsocketServer) MetaData(cid string) interface{} {
	ss.connMetaLock.Lock()
	defer ss.connMetaLock.Unlock()
	meta := ss.connectionMetaData[cid]
	return meta
}

// ActiveConnections returns all the active connections that the server currently has
func (ss *WebsocketServer) ActiveConnections() []*SocketConnection {
	var connlist []*SocketConnection
	ss.connMapLock.Lock()
	for _, v := range ss.connectionMap {
		connlist = append(connlist, v)
	}
	ss.connMapLock.Unlock()
	return connlist
}

// ConnectionFromMetaData returns the connection associated with the metadata
func (ss *WebsocketServer) ConnectionFromMetaData(meta interface{}) (string, error) {
	ss.connMetaLock.Lock()
	defer ss.connMetaLock.Unlock()
	for k, v := range ss.connectionMetaData {
		if v == meta {
			ss.connMapLock.Lock()
			defer ss.connMapLock.Unlock()
			return ss.connectionMap[k].id, nil
		}
	}
	return "", errors.New("no connection found with the provided meta-data")
}

func (ss *WebsocketServer) activeConnectionCount() int {
	ss.connMapLock.Lock()
	size := len(ss.connectionMap)
	ss.connMapLock.Unlock()
	return size
}

func (ss *WebsocketServer) registerConnection(conn *SocketConnection) {
	if conn != nil {
		ss.log.Printf("registering connection (id: %s) in the socketserver connection map", conn.id)
		ss.connMapLock.Lock()
		ss.connectionMap[conn.id] = conn
		ss.connMapLock.Unlock()
	}
}

func (ss *WebsocketServer) unregisterConnection(conn *SocketConnection) {
	if conn != nil {
		ss.log.Printf("unregistering connection (id: %s) in the socketserver connection map", conn.id)
		ss.connMapLock.Lock()
		delete(ss.connectionMap, conn.id)
		ss.connMapLock.Unlock()
		ss.connMetaLock.Lock()
		delete(ss.connectionMetaData, conn.id)
		ss.connMetaLock.Unlock()
	}
}

// ConnectionFromID returns the SocketConnection object from the connection-id
func (ss *WebsocketServer) ConnectionFromID(id string) *SocketConnection {
	return ss.connectionFromConnID(id)
}

// EventStream is the socket server's event stream
func (ss *WebsocketServer) EventStream() (<-chan ConnectionEvent, error) {
	ss.hasSubscriber.setTrue()
	return ss.eventStream, nil
}

func (ss *WebsocketServer) websocketHandler(w http.ResponseWriter, r *http.Request) {
	ss.log.Println("connection request received")
	c, err := ss.upgrader.Upgrade(w, r, nil)
	if err != nil {
		ss.log.Print("upgrade:", err)
		return
	}
	// generate a unique connection-id for this connection

	// we now have a websocket connection. Initiate the handshake and close the websocket connection if the handshake fails

	// ToDo Handshake to get connection id
	connectionID := uuid.NewV4().String()
	clientMetaData, err := ss.startServerHandshake(c, connectionID)
	if err != nil {
		// cleanup and get out of here
		panic(err)
	}
	ss.connMetaLock.Lock()
	ss.connectionMetaData[connectionID] = clientMetaData
	ss.connMetaLock.Unlock()
	opts := connectionOpts{
		Conn:        c,
		ID:          connectionID,
		KeepAlive:   ss.keepAlive,
		PingHandler: ss.pingHdlr,
		PongHandler: ss.pongHdlr,
		AppData:     ss.appData,
		Logger:      ss.log,
	}

	conn := NewSocketConnection(opts)
	conn.setType(ServerSide)
	conn.setSocketServer(ss)
	ss.registerConnection(conn)
	// start heartbeat
	if conn.heartBeat != nil {
		ss.log.Println("starting heartbeat")
		conn.heartBeat.start()
	}
	if ss.hasSubscriber.isSet() {
		ss.eventStream <- ConnectionEvent{
			EventType:    ConnectionReceived,
			ConnectionId: conn.id,
		}
	}

	ss.log.Println("connection established")
}

func (ss *WebsocketServer) startServerHandshake(c *websocket.Conn, connID string) (interface{}, error) {
	handshakeMeta := &clientHandshakeMetaData{}
	if err := c.ReadJSON(handshakeMeta); err != nil {
		return nil, err
	}
	if handshakeMeta.Type != clientHandshakeMetaDataFrame {
		return nil, errors.New("handshake protocol error during getting metadata")
	}
	if err := c.WriteJSON(&serverHandshakeConnectionID{
		Type:         serverHandshakeConnectionIDFrame,
		ConnectionID: connID,
	}); err != nil {
		return nil, err
	}
	ack := &clientHandshakeAck{}
	if err := c.ReadJSON(ack); err != nil {
		return nil, err
	}
	if ack.Type != clientHandshakeAckFrame {
		return nil, errors.New("handshake protocol error during ack")
	}
	return handshakeMeta.Meta, nil
}

func (ss *WebsocketServer) connectionFromConnID(id string) *SocketConnection {
	ss.connMapLock.Lock()
	c, ok := ss.connectionMap[id]
	ss.connMapLock.Unlock()
	if !ok {
		return nil
	}
	return c
}

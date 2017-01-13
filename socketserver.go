package restwebsocket

import (
	"errors"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/satori/go.uuid"
)

const (
	handshakeTimeout = 60 * time.Second
)

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
	connectionCh       chan *SocketConnection
	register           chan *SocketConnection
	unregister         chan *SocketConnection
}

type Envelope struct {
	CorrelationID string
	Payload       []byte
}

func NewWebSocketServer(addr string, maxConn int, keepAlive bool, pingHdlr, pongHdlr func(string) error, appData []byte) *WebsocketServer {
	srvr := &WebsocketServer{
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
		addr:               addr,
		keepAlive:          keepAlive,
		pingHdlr:           pingHdlr,
		pongHdlr:           pongHdlr,
		appData:            appData,
		connectionMap:      make(map[string]*SocketConnection),
		connectionMetaData: make(map[string]interface{}),
		register:           make(chan *SocketConnection),
		unregister:         make(chan *SocketConnection),
	}
	srvr.Manage()
	return srvr
}

func (ss *WebsocketServer) activeConnectionCount() int {
	ss.connMapLock.Lock()
	size := len(ss.connectionMap)
	ss.connMapLock.Unlock()
	return size
}

func (ss *WebsocketServer) Manage() {
	go ss.manage()
}

func (ss *WebsocketServer) manage() {
	for {
		select {
		case conn := <-ss.register:
			if conn != nil {
				log.Printf("registering connection (id: %s) in the socketserver connection map", conn.id)
				ss.connMapLock.Lock()
				ss.connectionMap[conn.id] = conn
				ss.connMapLock.Unlock()
			}

		case conn := <-ss.unregister:
			if conn != nil {
				log.Printf("unregistering connection (id: %s) in the socketserver connection map", conn.id)
				ss.connMapLock.Lock()
				delete(ss.connectionMap, conn.id)
				ss.connMapLock.Unlock()
			}
		}

		// Add broadcast
	}
}

func (ss *WebsocketServer) Accept() (<-chan *SocketConnection, error) {
	ch := make(chan *SocketConnection)
	ss.connectionCh = ch
	http.HandleFunc("/", ss.websocketHandler)
	go http.ListenAndServe(ss.addr, nil)
	return ch, nil
}

func (ss *WebsocketServer) websocketHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("connection request received")
	c, err := ss.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
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
	conn := NewSocketConnection(c, connectionID, ss.keepAlive, ss.pingHdlr, ss.pongHdlr, ss.appData)
	conn.setType(ServerSide)
	conn.setSocketServer(ss)
	ss.register <- conn
	// start heartbeat
	if conn.heartBeat != nil {
		log.Println("starting heartbeat")
		conn.heartBeat.start()
	}
	ss.connectionCh <- conn
	log.Println("connection established")
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

func (ss *WebsocketServer) Connection(id string) *SocketConnection {
	ss.connMapLock.Lock()
	c, ok := ss.connectionMap[id]
	ss.connMapLock.Unlock()
	if !ok {
		return nil
	}
	return c
}

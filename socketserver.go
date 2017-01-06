package restwebsocket

import (
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

type WebsocketServer struct {
	upgrader           websocket.Upgrader
	addr               string
	connectionMap      map[string]*SocketConnection
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
		addr:          addr,
		keepAlive:     keepAlive,
		pingHdlr:      pingHdlr,
		pongHdlr:      pongHdlr,
		appData:       appData,
		connectionMap: make(map[string]*SocketConnection),
		register:      make(chan *SocketConnection),
		unregister:    make(chan *SocketConnection),
	}
	srvr.Manage()
	return srvr
}

func (ss *WebsocketServer) Manage() {
	go ss.manage()
}

func (ss *WebsocketServer) manage() {
	for {
		select {
		case conn := <-ss.register:
			if conn != nil {
				ss.connectionMap[conn.id] = conn
			}

		case conn := <-ss.unregister:
			if conn != nil {
				//what is conn.send
				//close(conn.send)
				delete(ss.connectionMap, conn.id)
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
	log.Println("received request")
	c, err := ss.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}
	// ToDo Handshake to get connection id

	conn := NewSocketConnection(c, "dummyID", ss.keepAlive, ss.pingHdlr, ss.pongHdlr, ss.appData)
	conn.setType(ServerSide)
	conn.setSocketServer(ss)
	ss.register <- conn
	// start heartbeat
	if conn.heartBeat != nil {
		conn.heartBeat.start()
	}
	ss.connectionCh <- conn
	log.Println("connection established")
}

//
func (ss *WebsocketServer) Connection(id string) *SocketConnection {
	return ss.connectionMap[id]
}

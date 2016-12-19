package restwebsocket

import (
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

type websocketServer struct {
	upgrader           websocket.Upgrader
	addr               string
	connectionMap      map[string]*reliableSocketConnection
	keepAlive          bool
	pingHdlr, pongHdlr func(string) error
	appData            []byte
	handlerLoop        func()
	isRestapiServer    bool
	apiHdlr            http.Handler
	connectionCh       chan SocketConnection
	register           chan *reliableSocketConnection
	unregister         chan *reliableSocketConnection
}

type Envelope struct {
	CorrelationID string
	Payload       []byte
}

func NewWebSocketServer(addr string, maxConn int, keepAlive bool, pingHdlr, pongHdlr func(string) error, appData []byte) RestSocketServer {
	srvr := &websocketServer{
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
		addr:          addr,
		keepAlive:     keepAlive,
		pingHdlr:      pingHdlr,
		pongHdlr:      pongHdlr,
		appData:       appData,
		connectionMap: make(map[string]*reliableSocketConnection),
		register:      make(chan *reliableSocketConnection),
		unregister:    make(chan *reliableSocketConnection),
	}
	srvr.Manage()
	return srvr
}

func (ss *websocketServer) Manage() {
	go ss.manage()
}

func (ss *websocketServer) manage() {
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

func (ss *websocketServer) Accept() (<-chan SocketConnection, error) {
	ch := make(chan SocketConnection)
	ss.connectionCh = ch
	http.HandleFunc("/", ss.websocketHandler)
	go http.ListenAndServe(ss.addr, nil)
	return ch, nil
}

func (ss *websocketServer) websocketHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("received request")
	c, err := ss.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}
	// ToDo Handshake to get connection id

	conn := newReliableSocketConnection(c, "dummyID", ss.keepAlive, ss.pingHdlr, ss.pongHdlr, ss.appData)
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
func (ss *websocketServer) Connection(id string) SocketConnection {
	return ss.connectionMap[id]
}

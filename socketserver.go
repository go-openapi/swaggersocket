package restwebsocket

import (
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{}

type websocketServer struct {
	upgrader           websocket.Upgrader
	addr               string
	activeConnections  int
	maxConnections     int
	connectionMap      map[string]*reliableSocketConnection
	keepAlive          bool
	pingHdlr, pongHdlr func(string) error
	appData            []byte
	handlerLoop        func()
	isRestapiServer    bool
	apiHdlr            http.Handler
	connectionCh       chan SocketConnection
}

func NewWebSocketServer(addr string, maxConn int, keepAlive bool, pingHdlr, pongHdlr func(string) error, appData []byte) RestSocketServer {
	return &websocketServer{
		//ToDo: assign a more meaningful upgrader
		upgrader:          websocket.Upgrader{},
		addr:              addr,
		maxConnections:    maxConn,
		activeConnections: 0,
		keepAlive:         keepAlive,
		pingHdlr:          pingHdlr,
		pongHdlr:          pongHdlr,
		appData:           appData,
		connectionMap:     make(map[string]*reliableSocketConnection),
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
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}
	// ToDo Handshake to get connection id

	conn := newReliableSocketConnection(c, "dummyID", ss.keepAlive, ss.pingHdlr, ss.pongHdlr, ss.appData)
	conn.setType(ServerSide)
	conn.setSocketServer(ss)
	ss.connectionMap["dummyID"] = conn
	// BookKeeping stuff
	ss.activeConnections++
	// ToDo check if you passed maximum connections

	// start heartbeat
	if conn.HeartBeat() != nil {
		conn.HeartBeat().start()
	}
	ss.connectionCh <- conn
	log.Println("connection established")
}

func (ss *websocketServer) Connection(id string) SocketConnection {
	return ss.connectionMap[id]
}

func (ss *websocketServer) removeConnection(id string) {
	delete(ss.connectionMap, id)
}

func (ss *websocketServer) addConnection(id string, c *reliableSocketConnection) {
	ss.connectionMap[id] = c
}

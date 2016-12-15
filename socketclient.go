package restwebsocket

import (
	"log"
	"net/url"

	"github.com/gorilla/websocket"
)

type websocketClient struct {
	conn            SocketConnection
	handlerLoop     func()
	isRestapiServer bool
	keepAlive       bool
	pingHdlr        func(string) error
	pongHdlr        func(string) error
	appData         []byte
}

func NewWebSocketClient(keepAlive bool, pingHdlr, pongHdlr func(string) error, appData []byte) RestSocketClient {
	return &websocketClient{
		keepAlive: keepAlive,
		pingHdlr:  pingHdlr,
		pongHdlr:  pongHdlr,
		appData:   appData,
	}
}

func (sc *websocketClient) Connect(u *url.URL) error {
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Println("dial:", err)
		return err
	}
	// Connection established. Start Handshake
	// TODO HANDSHAKE
	// if successful handshake, set the reliable connection
	//ToDo Handshake to get ID

	//////

	sc.conn = newReliableSocketConnection(conn, "dummyConnectionId", sc.keepAlive, sc.pingHdlr, sc.pongHdlr, sc.appData)
	// start the heartbeat protocol
	if sc.conn.HeartBeat() != nil {
		sc.conn.HeartBeat().start()
	}
	return nil
}

func (sc *websocketClient) Connection() SocketConnection {
	return sc.conn
}

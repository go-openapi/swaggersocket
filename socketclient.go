package restwebsocket

import (
	"log"
	"net/url"

	"github.com/cenkalti/backoff"
	"github.com/gorilla/websocket"
)

type WebsocketClient struct {
	conn            *SocketConnection
	addr            *url.URL
	handlerLoop     func()
	isRestapiServer bool
	keepAlive       bool
	pingHdlr        func(string) error
	pongHdlr        func(string) error
	appData         []byte
}

func NewWebSocketClient(u *url.URL, keepAlive bool, pingHdlr, pongHdlr func(string) error, appData []byte) *WebsocketClient {
	return &WebsocketClient{
		keepAlive: keepAlive,
		pingHdlr:  pingHdlr,
		pongHdlr:  pongHdlr,
		appData:   appData,
		addr:      u,
	}
}

func (sc *WebsocketClient) Connect() error {
	operation := func() error {
		conn, _, err := websocket.DefaultDialer.Dial(sc.addr.String(), nil)
		if err != nil {
			log.Println("dial:", err)
			return err
		}
		// Connection established. Start Handshake
		// TODO HANDSHAKE
		// if successful handshake, set the reliable connection
		//ToDo Handshake to get ID

		//////

		c := NewSocketConnection(conn, "dummyConnectionId", sc.keepAlive, sc.pingHdlr, sc.pongHdlr, sc.appData)
		c.setSocketClient(sc)
		c.setType(ClientSide)
		sc.conn = c
		// start the heartbeat protocol
		if c.HeartBeat() != nil {
			c.HeartBeat().start()
		}
		return nil
	}

	err := backoff.Retry(operation, backoff.NewExponentialBackOff())
	if err != nil {
		log.Println("cannot initiate connection")
		return err
	}

	return nil
}

func (sc *WebsocketClient) Connection() *SocketConnection {
	return sc.conn
}

package swaggersocket

import (
	"errors"
	"log"
	"net/url"
	"sync"

	"github.com/cenkalti/backoff"
	"github.com/gorilla/websocket"
)

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
}

// NewWebSocketClient creates a new websocketcient
func NewWebSocketClient(u *url.URL, keepAlive bool, pingHdlr, pongHdlr func(string) error, appData []byte) *WebsocketClient {
	return &WebsocketClient{
		keepAlive: keepAlive,
		pingHdlr:  pingHdlr,
		pongHdlr:  pongHdlr,
		appData:   appData,
		addr:      u,
	}
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
			log.Println("dial:", err)
			return err
		}
		// Connection established. Start Handshake
		connectionID, err := sc.startClientHandshake(conn, sc.meta)
		if err != nil {
			// handle error and close the connection
			panic(err)
		}

		c := NewSocketConnection(conn, connectionID, sc.keepAlive, sc.pingHdlr, sc.pongHdlr, sc.appData)
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
		log.Println("cannot initiate connection")
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

func (sc *WebsocketClient) Connection() *SocketConnection {
	sc.connMutex.Lock()
	c := sc.conn
	sc.connMutex.Unlock()
	return c
}

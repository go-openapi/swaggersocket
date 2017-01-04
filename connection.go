package restwebsocket

import (
	"bufio"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// ConnectionType is the socket connection type, it can either be a serverside connection or a clientside connection
type ConnectionType int

const (
	// ServerSide means this connection is owned by a websocket-server
	ServerSide ConnectionType = iota
	// ClientSide means this connection is owned by a websocket-client
	ClientSide
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 20 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 512
)

// SocketConnection is a websocket a wrapper around the websocket connection
type SocketConnection interface {
	Close() error
	// return the connection id
	ID() string
	WriteRequest(req *http.Request) error
	WriteRaw([]byte) error
	ReadResponse() (*http.Response, error)
	ReadRequest() (*http.Request, error)
	HeartBeat() *heartbeat
}

// RestSocketClient is the websocket client that initiates the connection
type RestSocketClient interface {
	Connect() error
	Connection() SocketConnection
}

// RestSocketServer is the websocket server that listens to incoming connections
type RestSocketServer interface {
	// Connection returns the socket connection with the passed ID
	Connection(string) SocketConnection
	Accept() (<-chan SocketConnection, error)
}

// RestServer is a websocket connection that is a RESTAPI server
type RestServer interface {
	SocketConnection
	Serve() error
}

// reliableRestSocket is the implementation
type reliableSocketConnection struct {
	socketserver *websocketServer
	socketclient *websocketClient
	connType     ConnectionType
	conn         *websocket.Conn
	id           string
	heartBeat    *heartbeat
}

func newReliableSocketConnection(c *websocket.Conn, id string, keepAlive bool, pingHdlr, pongHdlr func(string) error, appData []byte) *reliableSocketConnection {
	// Default ping handler is to send back a pong control message with the same application data
	// Default pong handler is to do nothing
	// websocket protocol mentions that the pong message should reply back with the exact appData recieved from the ping message
	if pingHdlr != nil {
		c.SetPingHandler(pingHdlr)
	}
	if pongHdlr != nil {
		c.SetPongHandler(pingHdlr)
	}
	var hb *heartbeat
	if keepAlive {
		// create a new heartbeat object
		hb = newHeartBeat(c, heartbeatPeriod, pingwriteWait, appData)
	}
	return &reliableSocketConnection{
		conn:      c,
		heartBeat: hb,
		id:        id,
	}
}

func (c *reliableSocketConnection) setType(t ConnectionType) {
	c.connType = t
}

func (c *reliableSocketConnection) setSocketServer(s *websocketServer) {
	c.socketserver = s
}

func (c *reliableSocketConnection) setSocketClient(s *websocketClient) {
	c.socketclient = s
}

func (c *reliableSocketConnection) SocketServer() RestSocketServer {
	return c.socketserver
}

func (c *reliableSocketConnection) SocketClient() RestSocketClient {
	return c.socketclient
}

func (c *reliableSocketConnection) Type() ConnectionType {
	return c.connType
}

func (c *reliableSocketConnection) HeartBeat() *heartbeat {
	return c.heartBeat
}

func (c *reliableSocketConnection) handleFailure() {
	log.Println("connection failure detected")
	if c.heartBeat != nil {
		c.heartBeat.stop()
	}
	if c.connType == ServerSide {
		log.Printf("removing connection with id %s from connection list\n", c.id)
		// remove this connection from the server connectionMap
		c.socketserver.unregister <- c
	} else {
		// try to reconnect with exponential backoff
		c.socketclient.Connect()
	}
}

// Close provides a graceful termination of the connection
func (c *reliableSocketConnection) Close() error {
	// stop the heartbeat protocol
	c.heartBeat.stop()
	// some more stuff to do before closing the connection
	return c.conn.Close()
}

func (c *reliableSocketConnection) ID() string {
	return c.id
}

// WriteRequest writes a request to the underlying connection
func (c *reliableSocketConnection) WriteRequest(req *http.Request) error {
	var err error
	var w io.WriteCloser
	if w, err = c.conn.NextWriter(websocket.TextMessage); err == nil {
		defer w.Close()
		if err = req.Write(w); err == nil {
			return nil
		}
	}
	log.Printf("error: %v", err)
	if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway) {
		c.handleFailure()
	}
	return err
}

// WriteRequest writes a request to the underlying connection
func (c *reliableSocketConnection) WriteResponse(resp *http.Response) error {
	var err error
	var w io.WriteCloser
	if w, err = c.conn.NextWriter(websocket.TextMessage); err == nil {
		defer w.Close()
		if err = resp.Write(w); err == nil {
			return nil
		}
	}
	log.Printf("error: %v", err)
	if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway) {
		c.handleFailure()
	}
	return err
}

// this is how the connection read an http request
func (c *reliableSocketConnection) ReadRequest() (*http.Request, error) {
	var reader io.Reader
	var err error
	var mt int
	var req *http.Request

	if mt, reader, err = c.conn.NextReader(); err == nil {
		if mt != websocket.TextMessage {
			log.Println("error: not a text message")
			return nil, errors.New("not a text message")
		}
		if req, err = http.ReadRequest(bufio.NewReader(reader)); err == nil {
			return req, nil
		}
	}
	log.Printf("error: %v", err)
	if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway) {
		c.handleFailure()
	}
	return nil, err
}

// ResponseReader is used to read an http response
type ResponseReader struct {
	c *reliableSocketConnection
	r io.Reader
}

func (rr *ResponseReader) Read(p []byte) (int, error) {
	if rr.r == nil {
		_, reader, err := rr.c.conn.NextReader()
		if err != nil {
			// handle error
		}
		rr.r = reader
	}
	count, err := rr.r.Read(p)
	// this is a fake EOF sent because of a flush at the server side
	if count == 0 && err == io.EOF {
		log.Printf("I am in the count=0 path")
		_, reader, err := rr.c.conn.NextReader()
		if err != nil {
			// handle error
		}
		rr.r = reader
		// the correct count and EOF if any will be sent from here
		return rr.r.Read(p)
	}
	return count, err
}

func newResponseReader(c *reliableSocketConnection) io.Reader {
	return &ResponseReader{
		c: c,
	}
}

// ReadResponse reads a response from the underlying connection
func (c *reliableSocketConnection) ReadResponse() (*http.Response, error) {
	var err error
	var resp *http.Response
	if resp, err = http.ReadResponse(bufio.NewReader(newResponseReader(c)), nil); err == nil {
		return resp, nil
	}

	log.Printf("error: %v", err)
	if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway) {
		c.handleFailure()
	}
	return nil, err
}

// WriteRaw writes generic bytes to the connection
func (c *reliableSocketConnection) WriteRaw(b []byte) error {
	if err := c.conn.WriteMessage(websocket.TextMessage, b); err != nil {
		log.Printf("error: %v", err)
		if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway) {
			c.handleFailure()
		}
		return err
	}
	return nil
}

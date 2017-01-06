package restwebsocket

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
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

// SocketConnection is a wrapper around the websocket connection to handle http
type SocketConnection struct {
	socketserver        *WebsocketServer
	socketclient        *WebsocketClient
	connType            ConnectionType
	conn                *websocket.Conn
	id                  string
	heartBeat           *heartbeat
	closeNotificationCh chan bool
	once                sync.Once
}

// NewSocketConnection creates a new socket connection
func NewSocketConnection(c *websocket.Conn, id string, keepAlive bool, pingHdlr, pongHdlr func(string) error, appData []byte) *SocketConnection {
	// Default ping handler is to send back a pong control message with the same application data
	// Default pong handler is to do nothing
	// websocket protocol mentions that the pong message should reply back with the exact appData recieved from the ping message
	if pingHdlr != nil {
		c.SetPingHandler(pingHdlr)
	}
	if pongHdlr != nil {
		c.SetPongHandler(pingHdlr)
	}

	sockconn := &SocketConnection{
		conn:                c,
		id:                  id,
		closeNotificationCh: make(chan bool),
	}
	if keepAlive {
		// create a new heartbeat object
		sockconn.heartBeat = newHeartBeat(sockconn, heartbeatPeriod, pingwriteWait, appData)
	}
	return sockconn
}

func (c *SocketConnection) setType(t ConnectionType) {
	c.connType = t
}

func (c *SocketConnection) setSocketServer(s *WebsocketServer) {
	c.socketserver = s
}

func (c *SocketConnection) setSocketClient(s *WebsocketClient) {
	c.socketclient = s
}

func (c *SocketConnection) SocketServer() *WebsocketServer {
	return c.socketserver
}

func (c *SocketConnection) SocketClient() *WebsocketClient {
	return c.socketclient
}

func (c *SocketConnection) Type() ConnectionType {
	return c.connType
}

func (c *SocketConnection) HeartBeat() *heartbeat {
	return c.heartBeat
}

func (c *SocketConnection) handleFailure() {
	log.Println("connection failure detected")
	if c.heartBeat != nil {
		c.heartBeat.stop()
	}
	fmt.Println("I am here")
	if c.connType == ServerSide {
		//go c.once.Do(func() { c.closeNotificationCh <- true })
		log.Printf("removing connection with id %s from connection list\n", c.id)
		// remove this connection from the server connectionMap
		c.socketserver.unregister <- c
	} else {
		// try to reconnect with exponential backoff
		c.socketclient.Connect()
	}
}

// Close provides a graceful termination of the connection
func (c *SocketConnection) Close() error {
	// stop the heartbeat protocol
	if c.heartBeat != nil {
		c.heartBeat.stop()
	}
	// some more stuff to do before closing the connection
	return c.conn.Close()
}

func (c *SocketConnection) ID() string {
	return c.id
}

// WriteRequest writes a request to the underlying connection
func (c *SocketConnection) WriteRequest(req *http.Request) error {
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

// readRequest is how the connection read an http request and returns an http response
func (c *SocketConnection) readRequest(ctx context.Context) (*response, error) {
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
			ctx, cancelCtx := context.WithCancel(ctx)
			req = req.WithContext(ctx)
			w := &response{
				conn:          c,
				cancelCtx:     cancelCtx,
				req:           req,
				reqBody:       req.Body,
				handlerHeader: make(http.Header),
				contentLength: -1,
			}
			w.cw.res = w
			bufw, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				// handle failure
			}
			w.cw.writer = bufw
			w.w = newBufioWriterSize(&w.cw, bufferBeforeChunkingSize)
			return w, nil
		}
	}
	return nil, err
}

// ResponseReader is a specialized reader that reads streams on websockets
type ResponseReader struct {
	c *SocketConnection
	r io.Reader
}

func (rr *ResponseReader) Read(p []byte) (int, error) {
	if rr.r == nil {
		_, reader, err := rr.c.conn.NextReader()
		if err != nil {
			return 0, err
		}
		rr.r = reader
	}
	count, err := rr.r.Read(p)
	// this is a fake EOF sent because of a flush at the server side
	if count == 0 && err == io.EOF {
		_, reader, err := rr.c.conn.NextReader()
		if err != nil {
			log.Printf("Err: %v", err)
			return 0, err
		}
		rr.r = reader
		// the correct count and EOF if any will be sent from here
		return rr.r.Read(p)
	}
	return count, err
}

func newResponseReader(c *SocketConnection) io.Reader {
	return &ResponseReader{
		c: c,
	}
}

// ReadResponse reads a response from the underlying connection
func (c *SocketConnection) ReadResponse() (*http.Response, error) {
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
func (c *SocketConnection) WriteRaw(b []byte) error {
	if err := c.conn.WriteMessage(websocket.TextMessage, b); err != nil {
		log.Printf("error: %v", err)
		if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway) {
			c.handleFailure()
		}
		return err
	}
	return nil
}

func (c *SocketConnection) Serve(ctx context.Context, hdlr http.Handler) error {
	ctx, cancelCtx := context.WithCancel(ctx)
	defer cancelCtx()
	for {
		resp, err := c.readRequest(ctx)
		if err != nil {
			log.Println("error reading from connection")
			return err
		}
		hdlr.ServeHTTP(resp, resp.req)
		resp.cancelCtx()
		resp.finishRequest()
	}
}

package restwebsocket

import (
	"bufio"
	"context"
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

	closeWriteWait = 10 * time.Second
)

// SocketConnection is a wrapper around the websocket connection to handle http
type SocketConnection struct {
	socketserver *WebsocketServer
	socketclient *WebsocketClient
	connType     ConnectionType
	conn         *websocket.Conn
	id           string
	heartBeat    *heartbeat
	// The close notificationChannel is only created if this connection serves an api
	closeNotificationCh chan bool
	closeHandlerCh      chan bool
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
		closeNotificationCh: nil,
		closeHandlerCh:      nil,
	}
	if keepAlive {
		// create a new heartbeat object
		sockconn.heartBeat = newHeartBeat(sockconn, heartbeatPeriod, pingwriteWait, appData)
	}
	c.SetCloseHandler(func(code int, text string) error {
		log.Println("Close message recieved from peer")
		log.Println("cleaning up connection resources")
		sockconn.cleanupConnection()
		return nil
	})
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

	c.cleanupConnection()
	if c.connType == ClientSide {
		// reconnect with exponential backoff
		c.socketclient.Connect()
	}
}

// cleanup connection prepares for closing the connection. It acts as the close handler for the websocket connection
func (c *SocketConnection) cleanupConnection() {
	// the sequence of operations is very imprtant
	defer log.Printf("Websocket connection closed")
	if c.closeHandlerCh != nil {
		c.closeHandlerCh <- true
	}
	if c.closeNotificationCh != nil {
		// this will block until the apiserver loop reads the value
		c.once.Do(func() { c.closeNotificationCh <- true })
	}
	// stop the heartbeat protocol.
	if c.heartBeat != nil {
		c.heartBeat.stop()
	}
	if c.connType == ServerSide {
		// remove this connection from the server connectionMap
		c.socketserver.unregister <- c
	}
	c.conn.Close()
}

// Close provides a graceful termination of the connection
func (c *SocketConnection) Close() error {
	if c.closeHandlerCh != nil {
		c.closeHandlerCh <- true
	}
	if c.closeNotificationCh != nil {
		// this will block until the apiserver loop exits
		// this is very important to happen before the heartbeat stop
		// causes race if the socket server wants to close the connection
		c.once.Do(func() { c.closeNotificationCh <- true })
	}
	if c.heartBeat != nil {
		c.heartBeat.stop()
	}

	if c.connType == ServerSide {
		// remove this connection from the server connectionMap
		c.socketserver.unregister <- c
	}
	// some more stuff to do before closing the connection
	// write a close control message to the peer so that the peer can cleanup the connection
	c.conn.WriteControl(websocket.CloseMessage, nil, time.Now().Add(closeWriteWait))
	// close the underlying network connection
	if err := c.conn.Close(); err != nil {
		log.Printf("closing websocket connection: %v", err)
		return err
	}
	return nil
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
	var req *http.Request
	if _, reader, err = c.conn.NextReader(); err == nil {
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
			log.Printf("reading error: %v", err)
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

func (c *SocketConnection) Serve(ctx context.Context, hdlr http.Handler) {
	go c.serve(ctx, hdlr)
}

func (c *SocketConnection) serve(ctx context.Context, hdlr http.Handler) {
	ctx, cancelCtx := context.WithCancel(ctx)
	defer cancelCtx()
	defer log.Println("exiting api-server loop")
	c.closeNotificationCh = make(chan bool)
	defer close(c.closeNotificationCh)
	requestCh := make(chan *response)
	defer close(requestCh)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-c.closeNotificationCh:
				return
			case resp := <-requestCh:
				// you can listen to the closenotification channel inside the handler because the response writer implements the closeNotfiy interface. This is useful for long running handlers such as log --follow
				hdlr.ServeHTTP(resp, resp.req)
				resp.cancelCtx()
				resp.finishRequest()
			}
		}
	}()
	go func() {
		defer wg.Done()
		for {
			resp, err := c.readRequest(ctx)
			if err != nil {
				return
			}
			requestCh <- resp
		}
	}()
	wg.Wait()
}

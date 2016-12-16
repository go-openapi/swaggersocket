package restwebsocket

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

const (
	websocketMaxConnections = 100
)

//////////////////////////////////////////////////////////////////////////////////////
/////////////////////////////// interfaces ///////////////////////////////////////////
//////////////////////////////////////////////////////////////////////////////////////

// ConnectionType is the socket connection type, it can either be a serverside connection or a clientside connection
type ConnectionType int

const (
	// ServerSide means this connection is owned by a websocket-server
	ServerSide ConnectionType = iota
	// ClientSide means this connection is owned by a websocket-client
	ClientSide
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

//////////////////////////////////////////////////////////////////////////////////////
/////////////////////////////// structs //////////////////////////////////////////////
//////////////////////////////////////////////////////////////////////////////////////

// restRequest and restResponse are used internally to marshal/unmarshal websocket json messages

// restRequest is the struct representing the websocket rest request message
type restRequest struct {
	// ID should be a unique id for this request. This is how we determine a response related to this particular request
	ID      string
	Headers http.Header
	Method  string
	// Path includes the query strings
	Path string
	Body []byte
}

// restResponse is the struct representing the websocket rest response message
type restResponse struct {
	Status    int
	Body      []byte
	HeaderMap http.Header
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

func (c *reliableSocketConnection) ReadRequest() (*http.Request, error) {
	mt, message, err := c.conn.ReadMessage()

	if err != nil {
		log.Println("read:", err)
		// abnormal closure, unexpected EOF, for example the client disappears
		if websocket.IsCloseError(err, 1006) {
			c.handleFailure()
		}
		return nil, err
	}

	if mt != websocket.TextMessage {
		return nil, nil
	}

	req := new(restRequest)
	if err := json.Unmarshal(message, &req); err != nil {
		log.Println("unmarshal request:", err)
	}

	r, err := http.NewRequest(req.Method, req.Path, bytes.NewBuffer(req.Body))
	if err != nil {
		return nil, err
	}
	// setup headers
	r.Header = req.Headers
	return r, nil
}

func (c *reliableSocketConnection) handleFailure() {
	log.Println("connection failure detected")
	if c.heartBeat != nil {
		c.heartBeat.stop()
	}
	if c.connType == ServerSide {
		log.Printf("removing connection with id %s from connection list\n", c.id)
		c.socketserver.removeConnection(c.id)
	} else {
		// try to reconnect
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
	// donot ignore the error
	bdy, err := ioutil.ReadAll(req.Body)
	if err != nil {
		log.Println("read request: ", err)
		return err
	}
	restReq := &restRequest{
		ID:      "whatever", //this should be a uuid
		Headers: req.Header,
		Method:  req.Method,
		Path:    req.URL.Path,
		Body:    bdy,
	}
	b, err := json.Marshal(restReq)
	if err != nil {
		log.Println("marshal request: ", err)
		return err
	}
	if err := c.conn.WriteMessage(websocket.TextMessage, b); err != nil {
		log.Println("cannot write request")
		if websocket.IsCloseError(err, 1006) {
			c.handleFailure()
		}
		return err
	}
	return nil
}

func (c *reliableSocketConnection) ReadResponse() (*http.Response, error) {
	mt, message, err := c.conn.ReadMessage()

	if err != nil {
		log.Println("read:", err)
		if websocket.IsCloseError(err, 1006) {
			c.handleFailure()
		}
		return nil, err
	}

	if mt != websocket.TextMessage {
		return nil, nil
	}

	resp := new(restResponse)
	if err := json.Unmarshal(message, &resp); err != nil {
		log.Println("unmarshal response:", err)
		return nil, err
	}

	return &http.Response{
		StatusCode: resp.Status,
		Header:     resp.HeaderMap,
		Body:       ioutil.NopCloser(bytes.NewBuffer(resp.Body)),
	}, nil
}

func (c *reliableSocketConnection) WriteRaw(b []byte) error {
	err := c.conn.WriteMessage(websocket.TextMessage, b)
	if websocket.IsCloseError(err, 1006) {
		c.handleFailure()
	}
	return err
}

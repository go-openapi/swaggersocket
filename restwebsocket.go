package restwebsocket

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"

	"github.com/gorilla/websocket"
)

const (
	websocketMaxConnections = 100
)

// FailureCallbackFunc is a function that is called on a connection failure
type FailureCallbackFunc func(interface{}) interface{}

// Default FailureCallBack should implement a reconnection with exponential backoff

//////////////////////////////////////////////////////////////////////////////////////
/////////////////////////////// interfaces ///////////////////////////////////////////
//////////////////////////////////////////////////////////////////////////////////////

// SocketConnection is a websocket a wrapper around the websocket connection
type SocketConnection interface {
	Close() error
	OnConnectionFailure(FailureCallbackFunc)
	// return the connection id
	ID() string
	WriteRequest(req *http.Request)
	WriteRaw([]byte) error
	ReadResponse() *http.Response
	ReadRequest() *http.Request
	HeartBeat() *heartbeat
}

// RestSocketClient is the websocket client that initiates the connection
type RestSocketClient interface {
	Connect(u *url.URL) error
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
	SocketResponseWriter() http.ResponseWriter
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
	conn            *websocket.Conn
	id              string
	failureCallBack FailureCallbackFunc
	heartBeat       *heartbeat
}

func newReliableSocketConnection(c *websocket.Conn, id string, keepAlive bool, pingHdlr, pongHdlr func(string) error, appData []byte) SocketConnection {
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

func (c *reliableSocketConnection) HeartBeat() *heartbeat {
	return c.heartBeat
}

func (c *reliableSocketConnection) ReadRequest() *http.Request {
	mt, message, err := c.conn.ReadMessage()

	if err != nil {
		log.Println("read:", err)
		return nil
	}

	if mt != websocket.TextMessage {
		return nil
	}

	req := new(restRequest)
	if err := json.Unmarshal(message, &req); err != nil {
		log.Println("unmarshal request:", err)
	}

	r, err := http.NewRequest(req.Method, req.Path, bytes.NewBuffer(req.Body))
	if err != nil {
		return nil
	}
	// setup headers
	r.Header = req.Headers
	return r
}

func (c *reliableSocketConnection) OnConnectionFailure(f FailureCallbackFunc) {
	c.failureCallBack = f
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
func (c *reliableSocketConnection) WriteRequest(req *http.Request) {
	// donot ignore the error
	bdy, _ := ioutil.ReadAll(req.Body)
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
	}
	if err := c.conn.WriteMessage(websocket.TextMessage, b); err != nil {
		log.Println("cannot write request")
	}
}

func (c *reliableSocketConnection) ReadResponse() *http.Response {
	mt, message, err := c.conn.ReadMessage()

	if err != nil {
		log.Println("read:", err)
		return nil
	}

	if mt != websocket.TextMessage {
		return nil
	}

	resp := new(restResponse)
	if err := json.Unmarshal(message, &resp); err != nil {
		log.Println("unmarshal response:", err)
	}

	return &http.Response{
		StatusCode: resp.Status,
		Header:     resp.HeaderMap,
		Body:       ioutil.NopCloser(bytes.NewBuffer(resp.Body)),
	}
}

func (c *reliableSocketConnection) WriteRaw(b []byte) error {
	return c.conn.WriteMessage(websocket.TextMessage, b)
}

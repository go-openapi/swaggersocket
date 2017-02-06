// +build clientintegration

// These tests are integration tests for when the api-server is served by the websocket client
package swaggersocket

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/satori/go.uuid"
	"github.com/stretchr/testify/assert"
)

var (
	socketserver *WebsocketServer
	socketclient *WebsocketClient
	done         chan struct{}
	debugCh      = make(chan string)
)

func simpleHandler(rw http.ResponseWriter, req *http.Request) {
	rw.Write([]byte("Hello, Dolores!"))
}

func chunkedHandler(rw http.ResponseWriter, req *http.Request) {
	flusher, ok := rw.(http.Flusher)
	if !ok {
		panic("expected http.ResponseWriter to be an http.Flusher")
	}
	for i := 1; i <= 10; i++ {
		fmt.Fprintf(rw, "Chunk #%d\n", i)
		flusher.Flush()
		time.Sleep(250 * time.Millisecond)
	}
}

func closeNotifiedChunkedHandler(rw http.ResponseWriter, req *http.Request) {
	flusher, ok := rw.(http.Flusher)
	if !ok {
		panic("expected http.ResponseWriter to be an http.Flusher")
	}
	notify := rw.(http.CloseNotifier).CloseNotify()
	for i := 1; i <= 10; i++ {

		select {
		case <-notify:
			debugCh <- "Handler was notified of the client close"
			log.Println("connection closed...exiting handler")
			return
		default:
			fmt.Fprintf(rw, "Chunk #%d\n", i)
			flusher.Flush()
			time.Sleep(1 * time.Second)
		}
	}
}

func startSocketServer() (*WebsocketServer, chan struct{}) {
	opts := SocketServerOpts{
		Addr:      ":9090",
		KeepAlive: true,
	}
	wsServer := NewWebSocketServer(opts)
	ch, err := wsServer.EventStream()
	if err != nil {
		log.Println("accept: ", err)
	}
	done := make(chan struct{})
	log.Println("socketserver waiting for connection")
	go func() {
		defer log.Printf("closing socketserver")
		for {
			select {
			case evt := <-ch:
				log.Printf("Connection Event Recieved: %s", evt.EventType.String())
			case <-done:
				return
			}
		}
	}()
	return wsServer, done
}

func TestMain(m *testing.M) {
	socketserver, done = startSocketServer()
	code := m.Run()
	close(done)
	os.Exit(code)
}

func connectClient() {
	u, _ := url.Parse("ws://localhost:9090/")
	mux := http.NewServeMux()
	mux.HandleFunc("/simple/", simpleHandler)
	mux.HandleFunc("/chunked/", chunkedHandler)
	mux.HandleFunc("/closenotifiedchunked/", closeNotifiedChunkedHandler)
	opts := SocketClientOpts{
		URL:       u,
		KeepAlive: true,
	}
	socketclient = NewWebSocketClient(opts)
	if err := socketclient.Connect(); err != nil {
		panic(err)
	}
	socketclient.Connection().Serve(context.Background(), mux)
	time.Sleep(1 * time.Second)
}

func TestSimpleHandlerSuccess(t *testing.T) {
	connectClient()
	id := socketclient.Connection().ID()
	c := socketserver.connectionFromConnID(id)
	assert.NotNil(t, c)
	for i := 0; i < 4; i++ {
		req, _ := http.NewRequest(http.MethodGet, "ws://localhost:9090/simple/", nil)
		cid := uuid.NewV4().String()
		req.Header.Set("X-Correlation-Id", cid)
		err := c.WriteRequest(req)
		assert.Nil(t, err)
		resp, err := c.ReadResponse()
		assert.Nil(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, cid, resp.Header.Get("X-Correlation-Id"))
		b, err := ioutil.ReadAll(resp.Body)
		defer resp.Body.Close()
		assert.Nil(t, err)
		log.Printf(string(b))
		assert.Equal(t, "Hello, Dolores!", string(b))
	}
	connID := c.ID()
	c.Close()
	time.Sleep(1 * time.Second)
	assert.Nil(t, socketserver.connectionFromConnID(connID))
}

func TestChunkedHandlerSuccess(t *testing.T) {
	connectClient()
	id := socketclient.Connection().ID()
	c := socketserver.connectionFromConnID(id)
	assert.NotNil(t, c)
	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest(http.MethodGet, "ws://localhost:9090/chunked/", nil)
		cid := uuid.NewV4().String()
		req.Header.Set("X-Correlation-Id", cid)
		err := c.WriteRequest(req)
		assert.Nil(t, err)
		resp, err := c.ReadResponse()
		assert.Nil(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, cid, resp.Header.Get("X-Correlation-Id"))
		readbuf := make([]byte, 4096)
		count := 1
		for {
			//line, err := reader.ReadBytes('\n')
			n, err := resp.Body.Read(readbuf)
			if n > 0 {
				log.Println(string(readbuf))
				assert.Equal(t, fmt.Sprintf("Chunk #%d\n", count), string(bytes.Trim(readbuf, "\x00")))
				count++
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				panic(err)
			}
		}
		resp.Body.Close()
	}
	connID := c.ID()
	c.Close()
	time.Sleep(1 * time.Second)
	assert.Nil(t, socketserver.connectionFromConnID(connID))
}

func TestCloseNotifiedChunkedHandlerSuccess(t *testing.T) {
	connectClient()
	id := socketclient.Connection().ID()
	c := socketserver.connectionFromConnID(id)
	assert.NotNil(t, c)
	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest(http.MethodGet, "ws://localhost:9090/closenotifiedchunked/", nil)
		cid := uuid.NewV4().String()
		req.Header.Set("X-Correlation-Id", cid)
		err := c.WriteRequest(req)
		assert.Nil(t, err)
		resp, err := c.ReadResponse()
		assert.Nil(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, cid, resp.Header.Get("X-Correlation-Id"))
		readbuf := make([]byte, 4096)
		count := 1
		for {
			//line, err := reader.ReadBytes('\n')
			n, err := resp.Body.Read(readbuf)
			if n > 0 {
				assert.Equal(t, fmt.Sprintf("Chunk #%d\n", count), string(bytes.Trim(readbuf, "\x00")))
				count++
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				panic(err)
			}
		}
		resp.Body.Close()
	}
	connID := c.ID()
	c.Close()
	time.Sleep(1 * time.Second)
	assert.Nil(t, socketserver.connectionFromConnID(connID))
}

func TestCloseNotifiedChunkedFailureServerSide(t *testing.T) {
	connectClient()
	id := socketclient.Connection().ID()
	c := socketserver.connectionFromConnID(id)
	assert.NotNil(t, c)
	quit := false
	var count int
	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest(http.MethodGet, "ws://localhost:9090/closenotifiedchunked/", nil)
		cid := uuid.NewV4().String()
		req.Header.Set("X-Correlation-Id", cid)
		err := c.WriteRequest(req)
		assert.Nil(t, err)
		resp, err := c.ReadResponse()
		assert.Nil(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, cid, resp.Header.Get("X-Correlation-Id"))
		readbuf := make([]byte, 4096)
		count = 1
		for {
			//line, err := reader.ReadBytes('\n')
			n, err := resp.Body.Read(readbuf)
			defer resp.Body.Close()
			if count == 3 {
				c.conn.UnderlyingConn().Close()
				quit = true
				break
			}
			if n > 0 {
				assert.Equal(t, fmt.Sprintf("Chunk #%d\n", count), string(bytes.Trim(readbuf, "\x00")))
				count++
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				panic(err)
			}
		}
		if quit == true {
			break
		}
	}
	select {
	case <-time.After(30 * time.Second):
		t.Fatal("timed out")
	case s := <-debugCh:
		assert.Equal(t, "Handler was notified of the client close", s)
	}
}

// Should expect the socketserver to unregister the connection
func TestGeneralFailureClientSide(t *testing.T) {
	connectClient()
	// disabling failure detection at the socketclient side
	socketclient.Connection().heartBeat.stop()
	// force close the underlying network connection
	connectionId := socketclient.Connection().ID()
	socketclient.conn.conn.UnderlyingConn().Close()
	// the server is expected to detect that and remove the connection from the connection map
	success := make(chan bool, 1)
	go func() {
		for {
			if socketserver.connectionFromConnID(connectionId) == nil {
				success <- true
				return
			}
			time.Sleep(1 * time.Second)
		}
	}()
	select {
	case <-time.After(30 * time.Second):
		t.Fatal("timed out")
	case <-success:
	}
}

func TestGeneralFailureServerSide(t *testing.T) {
	connectClient()
	socketclient.conn.conn.UnderlyingConn().Close()
	//time.Sleep(10 * time.Second)
	// client should try to reconnect with backoff
	success := make(chan bool, 1)
	go func() {
		for {
			if err := socketclient.Connection().conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)); err == nil {
				success <- true
				return
			}
			time.Sleep(1 * time.Second)
		}
	}()
	select {
	case <-time.After(20 * time.Second):
		t.Fatal("timed out")
	case <-success:
	}
}

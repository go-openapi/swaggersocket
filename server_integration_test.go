// +build serverintegration

// These tests are integration tests for when the api-server is served by the websocket server
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
	uuid "github.com/satori/go.uuid"
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
		panic(err)
	}
	m := http.NewServeMux()
	m.HandleFunc("/simple/", simpleHandler)
	m.HandleFunc("/chunked/", chunkedHandler)
	m.HandleFunc("/closenotifiedchunked/", closeNotifiedChunkedHandler)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case event := <-ch:
				if event.EventType == ConnectionReceived {
					conn := wsServer.connectionFromConnID(event.ConnectionId)
					conn.Serve(context.Background(), m)
				}

			case <-done:
				return
			}
		}
	}()
	return wsServer, done
}

func TestMain(m *testing.M) {
	socketserver, done = startSocketServer()
	u, _ := url.Parse("ws://localhost:9090/")
	opts := SocketClientOpts{
		URL:       u,
		KeepAlive: true,
	}
	socketclient = NewWebSocketClient(opts)
	code := m.Run()
	close(done)
	os.Exit(code)
}

func TestSimpleHandlerSuccess(t *testing.T) {
	err := socketclient.Connect()
	assert.Nil(t, err)
	for i := 0; i < 4; i++ {
		req, _ := http.NewRequest(http.MethodGet, "ws://localhost:9090/simple/", nil)
		cid := uuid.NewV4().String()
		req.Header.Set("X-Correlation-Id", cid)
		err := socketclient.Connection().WriteRequest(req)
		assert.Nil(t, err)
		resp, err := socketclient.Connection().ReadResponse()
		assert.Nil(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, cid, resp.Header.Get("X-Correlation-Id"))
		b, err := ioutil.ReadAll(resp.Body)
		defer resp.Body.Close()
		assert.Nil(t, err)
		assert.Equal(t, "Hello, Dolores!", string(b))
	}
	connID := socketclient.Connection().ID()
	socketclient.Connection().Close()
	// give some time for the server to unregister connection
	// ToDo find a better way to do this
	time.Sleep(1 * time.Second)
	assert.Nil(t, socketserver.connectionFromConnID(connID))
}

func TestChunkedHandlerSuccess(t *testing.T) {
	err := socketclient.Connect()
	assert.Nil(t, err)
	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest(http.MethodGet, "ws://localhost:9090/chunked/", nil)
		cid := uuid.NewV4().String()
		req.Header.Set("X-Correlation-Id", cid)
		err := socketclient.Connection().WriteRequest(req)
		assert.Nil(t, err)
		resp, err := socketclient.Connection().ReadResponse()
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
	connID := socketclient.Connection().ID()
	socketclient.Connection().Close()
	// give some time for the server to unregister connection
	// ToDo find a better way to do this
	time.Sleep(1 * time.Second)
	assert.Nil(t, socketserver.connectionFromConnID(connID))
}

func TestCloseNotifiedChunkedHandlerSuccess(t *testing.T) {
	err := socketclient.Connect()
	assert.Nil(t, err)
	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest(http.MethodGet, "ws://localhost:9090/closenotifiedchunked/", nil)
		cid := uuid.NewV4().String()
		req.Header.Set("X-Correlation-Id", cid)
		err := socketclient.Connection().WriteRequest(req)
		assert.Nil(t, err)
		resp, err := socketclient.Connection().ReadResponse()
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
	connID := socketclient.Connection().ID()
	socketclient.Connection().Close()
	// give some time for the server to unregister connection
	// ToDo find a better way to do this
	time.Sleep(1 * time.Second)
	assert.Nil(t, socketserver.connectionFromConnID(connID))
}

func TestCloseNotifiedChunkedFailureClientSide(t *testing.T) {
	err := socketclient.Connect()
	// disabling failure detection at the socketclient side
	socketclient.Connection().heartBeat.stop()
	assert.Nil(t, err)
	quit := false
	var count int
	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest(http.MethodGet, "ws://localhost:9090/closenotifiedchunked/", nil)
		cid := uuid.NewV4().String()
		req.Header.Set("X-Correlation-Id", cid)
		err := socketclient.Connection().WriteRequest(req)
		assert.Nil(t, err)
		resp, err := socketclient.Connection().ReadResponse()
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
				socketclient.conn.conn.UnderlyingConn().Close()
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

func TestGeneralFailureClientSide(t *testing.T) {
	err := socketclient.Connect()
	// disabling failure detection at the socketclient side
	connectionId := socketclient.Connection().ID()
	socketclient.Connection().heartBeat.stop()
	assert.Nil(t, err)
	// force close the underlying network connection
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
		log.Printf("time out")
		t.Fatal("timed out")
	case <-success:
	}
}

func TestGeneralFailureServerSide(t *testing.T) {
	err := socketclient.Connect()
	// disabling failure detection at the socketclient side
	assert.Nil(t, err)
	// force close the underlying network connection
	socketclient.conn.conn.UnderlyingConn().Close()
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

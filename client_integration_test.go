// +build clientintegration

// These tests are integration tests for when the api-server is served by the websocket client

package restwebsocket

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

	"github.com/stretchr/testify/assert"
)

var (
	socketserver *WebsocketServer
	socketclient *WebsocketClient
	done         chan struct{}
	debugStr     string
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
			debugStr = "Handler was notified of the client close"
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
	wsServer := NewWebSocketServer(":9090", 100, true, nil, nil, nil)
	ch, err := wsServer.Accept()
	if err != nil {
		log.Println("accept: ", err)
	}
	done := make(chan struct{})
	log.Println("socketserver waiting for connection")
	go func() {
		defer log.Printf("closing socketserver")
		for {
			select {
			case <-ch:
				log.Println("socket server received connection")
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
	socketclient = NewWebSocketClient(u, true, nil, nil, nil)
	if err := socketclient.Connect(); err != nil {
		panic(err)
	}
	socketclient.Connection().Serve(context.Background(), mux)
	time.Sleep(1 * time.Second)
}

func TestSimpleHandlerSuccess(t *testing.T) {
	connectClient()
	req, _ := http.NewRequest(http.MethodGet, "ws://localhost:9090/simple/", nil)
	assert.Equal(t, 1, len(socketserver.connectionMap))
	var c *SocketConnection
	for _, v := range socketserver.connectionMap {
		c = v
		break
	}
	assert.NotNil(t, c)
	for i := 0; i < 4; i++ {
		err := c.WriteRequest(req)
		assert.Nil(t, err)
		resp, err := c.ReadResponse()
		assert.Nil(t, err)
		assert.NotNil(t, resp)
		b, err := ioutil.ReadAll(resp.Body)
		defer resp.Body.Close()
		assert.Nil(t, err)
		log.Printf(string(b))
		assert.Equal(t, "Hello, Dolores!", string(b))
	}
	beforeCount := socketserver.activeConnectionCount()
	c.Close()
	time.Sleep(1 * time.Second)
	assert.Equal(t, beforeCount-1, socketserver.activeConnectionCount())
}

func TestChunkedHandlerSuccess(t *testing.T) {
	connectClient()
	req, _ := http.NewRequest(http.MethodGet, "ws://localhost:9090/chunked/", nil)
	assert.Equal(t, 1, len(socketserver.connectionMap))
	var c *SocketConnection
	for _, v := range socketserver.connectionMap {
		c = v
		break
	}
	assert.NotNil(t, c)
	for i := 0; i < 2; i++ {
		err := c.WriteRequest(req)
		assert.Nil(t, err)
		resp, err := c.ReadResponse()
		assert.Nil(t, err)
		assert.NotNil(t, resp)
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
	beforeCount := socketserver.activeConnectionCount()
	c.Close()
	// give some time for the server to unregister connection
	// ToDo find a better way to do this
	time.Sleep(1 * time.Second)
	assert.Equal(t, beforeCount-1, socketserver.activeConnectionCount())
}

func TestCloseNotifiedChunkedHandlerSuccess(t *testing.T) {
	connectClient()
	req, _ := http.NewRequest(http.MethodGet, "ws://localhost:9090/closenotifiedchunked/", nil)
	assert.Equal(t, 1, len(socketserver.connectionMap))
	var c *SocketConnection
	for _, v := range socketserver.connectionMap {
		c = v
		break
	}
	assert.NotNil(t, c)
	for i := 0; i < 2; i++ {
		err := c.WriteRequest(req)
		assert.Nil(t, err)
		resp, err := c.ReadResponse()
		assert.Nil(t, err)
		assert.NotNil(t, resp)
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
	beforeCount := socketserver.activeConnectionCount()
	c.Close()
	// give some time for the server to unregister connection
	// ToDo find a better way to do this
	time.Sleep(1 * time.Second)
	assert.Equal(t, beforeCount-1, socketserver.activeConnectionCount())
}

func TestCloseNotifiedChunkedFailureServerSide(t *testing.T) {
	connectClient()
	req, _ := http.NewRequest(http.MethodGet, "ws://localhost:9090/closenotifiedchunked/", nil)
	assert.Equal(t, 1, len(socketserver.connectionMap))
	var c *SocketConnection
	for _, v := range socketserver.connectionMap {
		c = v
		break
	}
	assert.NotNil(t, c)
	debugStr = "whatever"
	quit := false
	var count int
	for i := 0; i < 2; i++ {
		err := c.WriteRequest(req)
		assert.Nil(t, err)
		resp, err := c.ReadResponse()
		assert.Nil(t, err)
		assert.NotNil(t, resp)
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
	success := make(chan bool, 1)
	go func() {
		for {
			if debugStr == "Handler was notified of the client close" {
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

// Should expect the socketserver to unregister the connection
func TestGeneralFailureClientSide(t *testing.T) {
	connectClient()
	// disabling failure detection at the socketclient side
	socketclient.Connection().heartBeat.stop()
	// force close the underlying network connection
	beforeCount := socketserver.activeConnectionCount()
	socketclient.conn.conn.UnderlyingConn().Close()
	// the server is expected to detect that and remove the connection from the connection map
	success := make(chan bool, 1)
	go func() {
		for {
			if socketserver.activeConnectionCount() == beforeCount-1 {
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

// func TestGeneralFailureServerSide(t *testing.T) {
// 	time.Sleep(3 * time.Second)
// 	err := socketclient.Connect()
// 	// disabling failure detection at the socketclient side
// 	assert.Nil(t, err)
// 	// force close the underlying network connection
// 	//beforeConn := socketclient.Connection()
// 	socketclient.conn.conn.UnderlyingConn().Close()
// 	time.Sleep(10 * time.Second)
// 	// client should try to reconnect with backoff
// 	success := make(chan bool, 1)
// 	go func() {
// 		for {
// 			if err := socketclient.Connection().conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)); err == nil {
// 				success <- true
// 				return
// 			}
// 			time.Sleep(1 * time.Second)
// 		}
// 	}()
// 	select {
// 	case <-time.After(20 * time.Second):
// 		t.Fatal("timed out")
// 	case <-success:
// 	}
// }

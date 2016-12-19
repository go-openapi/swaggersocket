package restwebsocket

import (
	"bytes"
	"io/ioutil"
	"net/http"
)

// reliableRestServer is the implementation of the RestServer interface
type reliableRestServer struct {
	*reliableSocketConnection
	hdlr http.Handler
}

// NewRestServer creates a new rest api server
func NewRestServer(conn SocketConnection, handler http.Handler) RestServer {
	return &reliableRestServer{
		reliableSocketConnection: conn.(*reliableSocketConnection),
		hdlr: handler,
	}
}

func (s *reliableRestServer) Serve() error {
	for {
		req, err := s.ReadRequest()
		if err != nil {
			return err
		}

		rw := newsocketResponseWriter(s.reliableSocketConnection)
		s.hdlr.ServeHTTP(rw, req)
		//response := rw.close()
		//if err := s.WriteRaw(response); err != nil {
		//	log.Println("write message:", err)
		//}
	}
}

type socketResponseWriter struct {
	r    *http.Response
	conn *reliableSocketConnection
}

func (rw *socketResponseWriter) Header() http.Header {
	return rw.r.Header
}

func (rw *socketResponseWriter) WriteHeader(code int) {
	rw.r.StatusCode = code
}

func (rw *socketResponseWriter) Write(b []byte) (int, error) {
	rw.r.Body = ioutil.NopCloser(bytes.NewReader(b))
	if err := rw.conn.WriteResponse(rw.r); err != nil {
		return -1, err
	}
	return len(b), nil
}

func newsocketResponseWriter(c *reliableSocketConnection) *socketResponseWriter {
	return &socketResponseWriter{
		r:    new(http.Response),
		conn: c,
	}
}

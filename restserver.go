package restwebsocket

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

// reliableRestServer is the implementation of the RestServer interface
type reliableRestServer struct {
	*reliableSocketConnection
	rw   *restResponseWriter
	hdlr http.Handler
}

// NewRestServer creates a new rest api server
func NewRestServer(conn SocketConnection, handler http.Handler) RestServer {
	return &reliableRestServer{
		reliableSocketConnection: conn.(*reliableSocketConnection),
		rw:   newRestResponseWriter(),
		hdlr: handler,
	}
}

func (s *reliableRestServer) SocketResponseWriter() http.ResponseWriter {
	return s.rw
}

func (s *reliableRestServer) Serve() error {
	for {
		req, err := s.ReadRequest()

		if err != nil {
			// abnormal closure, unexpected EOF, for example the client disappears
			if websocket.IsCloseError(err, 1006) {
				s.handleFailure()
				return nil
			}
		}

		if err != nil {

		}

		if req == nil {
			log.Println("not a text message")
		}

		rw := s.rw
		s.hdlr.ServeHTTP(rw, req)
		response := rw.close()
		if err := s.WriteRaw(response); err != nil {
			log.Println("write message:", err)
		}
	}
}

type restResponseWriter struct {
	Status    int
	Buf       *bytes.Buffer
	HeaderMap http.Header
}

func (rw *restResponseWriter) Header() http.Header {
	return rw.HeaderMap
}

func (rw *restResponseWriter) WriteHeader(code int) {
	rw.Status = code
	// TODO: you should also write the headers here
}

func (rw *restResponseWriter) Write(b []byte) (int, error) {
	i, err := rw.Buf.Write(b)
	return i, err
}

func (rw *restResponseWriter) close() []byte {
	resp := &restResponse{
		Status:    rw.Status,
		Body:      rw.Buf.Bytes(),
		HeaderMap: rw.HeaderMap,
	}
	// Do the actual writing here
	b, _ := json.Marshal(resp)
	return b

}

func newRestResponseWriter() *restResponseWriter {
	var b []byte
	return &restResponseWriter{
		Buf:       bytes.NewBuffer(b),
		HeaderMap: make(http.Header),
	}
}

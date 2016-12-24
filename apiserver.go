package restwebsocket

import "net/http"

// apiServer is the implementation of the RestServer interface
type apiServer struct {
	*reliableSocketConnection
	hdlr http.Handler
}

// NewAPIServer creates a new rest api server
func NewAPIServer(conn SocketConnection, handler http.Handler) RestServer {
	return &apiServer{
		reliableSocketConnection: conn.(*reliableSocketConnection),
		hdlr: handler,
	}
}

func (s *apiServer) Serve() error {
	for {
		req, err := s.ReadRequest()
		if err != nil {
			return err
		}

		rw := s.newresponse(req)
		s.hdlr.ServeHTTP(rw, req)
		rw.finishRequest()
	}
}

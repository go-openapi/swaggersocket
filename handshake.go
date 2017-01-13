package restwebsocket

const (
	clientHandshakeMetaDataFrame     = "ClientHandshakeMetaData"
	clientHandshakeAckFrame          = "ClientHandshakeAckFrame"
	serverHandshakeConnectionIDFrame = "ServerHandshakeConnectionID"
)

type clientHandshakeMetaData struct {
	Type string
	Meta interface{}
}

type clientHandshakeAck struct {
	Type string
}

type serverHandshakeConnectionID struct {
	Type         string
	ConnectionID string
}

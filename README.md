# SwaggerSocket [![Build Status](https://travis-ci.org/go-openapi/swaggersocket.svg?branch=master)](https://travis-ci.org/go-openapi/swaggersocket) [![codecov](https://codecov.io/gh/go-openapi/swaggersocket/branch/master/graph/badge.svg)](https://codecov.io/gh/go-openapi/swaggersocket) [![Slack Status](https://slackin.goswagger.io/badge.svg)](https://slackin.goswagger.io)

[![license](http://img.shields.io/badge/license-Apache%20v2-orange.svg)](https://raw.githubusercontent.com/go-openapi/swaggersocket/master/LICENSE) [![GoDoc](https://godoc.org/github.com/go-openapi/swaggersocket?status.svg)](http://godoc.org/github.com/go-openapi/swaggersocket)

ReST over websocket, so you can serve swagger apis over websocket

## Websocket Server

### How to create a websocket server
A websocket server can be created using the `NewWebSocketServer` function
```go
func NewWebSocketServer(opts SocketServerOpts) *WebsocketServer
```
 The above function creates a websocket server based on the options structure passed to the constructor function

 ```go
 type SocketServerOpts struct {
	Addr      string
	KeepAlive bool
	PingHdlr  func(string) error
	PongHdlr  func(string) error
	AppData   []byte
	Log       Logger
}
```

`Addr` the ip address and port the websocket server is listening to
`KeepAlive` activates the heartbeat mechanism at the server side
`PingHdlr` A custom function to websocket ping messages (default is to send back a pong message)
`PongHdlr` a custom function to a websocket pong message (default is nothing)
`AppData` optional application-level data passed with the heartbeat messages
`Log` custom logger

### The Websocket Server Event Stream
After creating a websocket server, the user will probably need to listen the events happening on the websocket server.

To listen to the event stream of the websocket server
```go
func (ss *WebsocketServer) EventStream() (<-chan ConnectionEvent, error)
```
The `EventStream` method returs a channel that carries `ConnectionEvent` instances

```go
type ConnectionEvent struct {
	EventType    EvtType
	ConnectionId string
}
```

There are three types of connection events:
- ConnectionReceived
- ConnectionClosed
- ConnectionFailure

The `ConnectionEvent` also has the `ConnectionId` field which is the ID of the connection associated with the event.

## Websocket Client
A websocket client can be created using the `NewWebSocketServer` function
```go
func NewWebSocketClient(opts SocketClientOpts) *WebsocketClient
```
 This creates a websocket client based on the `SocketClientOpts`

 ```go
 type SocketClientOpts struct {
	URL       *url.URL
	KeepAlive bool
	PingHdlr  func(string) error
	PongHdlr  func(string) error
	AppData   []byte
	Logger    Logger
}
```

The `URL` is the url of the websocket server
A `KeepAlive` set to true will activate the heartbeat mechanism from the client side and on connection failure, will attempt to reconnect to the websocket server with an exponential backoff.

## Server-Client Handshake
This is a three-way handshake in the beginning of the connection. The purpose of this handshake is:

- agree on a unique id `ConnectionId` for the connection
- allowing the websocket client to store some connection metadata at the websocket server side

In particular, this is the sequence of events:

1. The websocket Client sends an initial message to the websocket server with the connection metadata (if any)
2. The websocket server stores the metadata for the connection and responds with a unique connection id
3. The client confirms the reception of the connection id

## The Heartbeat
The Heartbeat is the mechanism by which connection failures can be detected. The Heartbeat simply utilizes the underlying websocket ping/pong messages to send periodic heartbeat messages to check the health of the connection. Heartbeat messages can be activated at the websocket client and/or server sides by setting the `KeepAlive` field.

## Swagger API Server
Websocket connections are bidirectional. This means the API server can be attached to either the websocket client or the websocket server.

If the api server is attached to the websocket client, the websocket server will act as the api client (issue requests to the websocket client and receive ap response)

After a connection has been established between the websocket server and the websocket client, the api server can be attached to any by hooking up the swagger api handler.

### Attaching the Swagger API handler to the websocket server
```go
wsServer := NewWebSocketServer(opts)
ch, err := wsServer.EventStream()
// the following loop is safe to run in a separate go-routine
for {
			select {
			case event := <-ch:
				if event.EventType == ConnectionReceived {
					conn := wsServer.connectionFromConnID(event.ConnectionId)
					conn.Serve(context.Background(), SwaggerAPIHandler)
				}

			case <-done:
				return
			}
}
```
### Attaching the swagger api handler to the websocket client
```go
// create a websocket client
wsClient := swaggersocket.NewWebSocketClient(clientOpts).WithMetaData("dummy connection metadata")
// connect to the websocket server
if err := wsClient.Connect(); err != nil {
		panic(err)
}
///////////////////
// some code goes here
//////////////////
// serve the swagger api on the websocket client
wsClient.Connection().Serve(context.Background(), SwaggerAPIHandler)
```
## Swagger API Client
`SocketConnection` implements the RoundTripper interface. This means that the swagger client requires no modifications except for changing the RoundTripper of the runtime

For example, assuming that we are hooking the swagger client to the websocket client:

```go
// create a websocket client
wsClient := swaggersocket.NewWebSocketClient(clientOpts).WithMetaData("dummy connection metadata")
// connect to the websocket server
if err := wsClient.Connect(); err != nil {
		panic(err)
}
///////////////////
// some code goes here
//////////////////
// runtime is the go-openapi runtime
rt := runtime.New("localhost", "/", []string{"https"})
rt.Transport = wsClient.Connection()
// client is the swagger http client
cli := client.New(rt, nil)
```

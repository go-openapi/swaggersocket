# swaggersocket
ReST over websocket, so you can serve swagger apis over websocket

## How to create a websocket server
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
## How to create a websocket client

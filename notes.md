# Notes from review 1/17/2017

## Logger

Use an interface you pass in like

```go
type Logger interface {
  Print(...interface{})
  Printf(string, ...interface{})
  Println(...interface{})
}
```

## Connection Options

Change `NewSocketConnection(c *websocket.Conn, id string, keepAlive bool, pingHdlr, pongHdlr func(string) error, appData []byte) *SocketConnection`

to `NewSocketConnection(opts ConnectionOpts) *SocketConnection`

and connection opts is:

```go
type ConnectionOpts struct {
  Conn *websocket.Conn
  ID string
  KeepAlive bool
  PingHandler func(string) error
  PongHandler func(string) error
  AppData []byte
}
```

## Heartbeat type

the type heartbeat either needs to be an exported type or it needs to be unexported from the SocketConnection methods

Shouldn't the heartbeat be stopped before you do the close notifications in cleanup connection and the close?


## connection.go readRequest

it has err != nil, with an empty body, should we at least log that?

## connectiontype type

Perhaps this should get a `.String()` method so that you can get rid of a number of if statements that only have to do with logging.
eg. in heartbeat.



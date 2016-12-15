package restwebsocket

import (
	"log"
	"time"

	"github.com/gorilla/websocket"
)

const (
	heartbeatPeriod = 30 * time.Second
	pingwriteWait   = 5 * time.Second
)

// The reason for heartbeat is to to utilize the underlying websocket ping pong mechanism to keep connections alive
type heartbeat struct {
	conn          *websocket.Conn
	period        time.Duration
	pingWriteWait time.Duration
	pingMsg       []byte
	stopCh        chan struct{}
}

func newHeartBeat(c *websocket.Conn, period, pingwritewait time.Duration, appData []byte) *heartbeat {
	return &heartbeat{
		conn:          c,
		period:        period,
		pingWriteWait: pingwritewait,
		pingMsg:       appData,
		stopCh:        make(chan struct{}),
	}
}

func (hb *heartbeat) start() {
	go func() {
		// periodically send a ping control message
		ticker := time.NewTicker(hb.period)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				// write a websocket ping control message. WriteControl is safe to use concurrently
				log.Println("sending ping message")
				if err := hb.conn.WriteControl(websocket.PingMessage, hb.pingMsg, time.Now().Add(hb.pingWriteWait)); err != nil {
					log.Println("ping message: ", err)
				}
			case <-hb.stopCh:
				return
			}
		}
	}()

}

func (hb *heartbeat) stop() {
	close(hb.stopCh)
}

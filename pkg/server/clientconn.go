package server

import (
	"log"
	"strings"
	"sync"
	"time"

	"github.com/Fekinox/socket-server-test/pkg/message"
	"github.com/gorilla/websocket"
)

type HandlerFunc func(username string, data string)

type ClientConn struct {
	Username string

	conn     *websocket.Conn
	messages chan *AcknowledgedMessage

	handlers map[string]HandlerFunc

	closed   bool
	closedMu sync.Mutex
}

func (c *ClientConn) readPump() {
	defer c.conn.Close()

	c.conn.SetReadDeadline(time.Now().Add(PONG_WAIT_TIME))
	c.conn.SetPongHandler(func(string) error {
		log.Println("pong")
		c.conn.SetReadDeadline(time.Now().Add(PONG_WAIT_TIME))
		return nil
	})

	for {
		typ, msg, err := c.conn.ReadMessage()
		if err != nil {
			log.Println(err)
			return
		}
		if typ != websocket.TextMessage {
			continue
		}

		ident, body, _ := strings.Cut(string(msg), " ")

		if h, ok := c.handlers[ident]; ok {
			h(c.Username, body)
		}
	}
}

func (c *ClientConn) writePump() {
	ticker := time.NewTicker(PING_PERIOD)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case m, ok := <-c.messages:
			if !ok {
				continue
			}
			c.conn.SetWriteDeadline(time.Now().Add(WRITE_WAIT_TIME))
			err := func() error {
				if m.Type == websocket.TextMessage || m.Type == websocket.BinaryMessage {
					c.conn.SetWriteDeadline(time.Now().Add(WRITE_WAIT_TIME))
					return c.conn.WriteMessage(m.Type, m.Data)
				} else {
					return c.conn.WriteControl(
						m.Type,
						m.Data,
						time.Now().Add(PING_PERIOD),
					)
				}
			}()
			if m.ack != nil {
				m.ack <- err
				close(m.ack)
			}

		case <-ticker.C:
			err := c.conn.WriteControl(
				websocket.PingMessage,
				nil,
				time.Now().Add(WRITE_WAIT_TIME),
			)
			if err != nil {
				return
			}
			log.Println("ping")
		}
	}
}

func (c *ClientConn) On(ident string, f func(username string, body string)) {
	c.handlers[ident] = f
}

func (c *ClientConn) WriteTextMessage(data string) error {
	return c.WriteMessage(websocket.TextMessage, []byte(data))
}

func (c *ClientConn) WriteBinaryMessage(data []byte) error {
	return c.WriteMessage(websocket.BinaryMessage, []byte(data))
}

func (c *ClientConn) WriteCloseMessage(code int, text string) error {
	return c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(code, text))
}

func (c *ClientConn) WriteMessage(typ int, data []byte) error {
	msg := AcknowledgedMessage{
		Message: message.Message{
			Type: typ,
			Data: data,
		},
		ack: make(chan error),
	}

	c.messages <- &msg
	return <-msg.ack
}

func (c *ClientConn) MarkClosed(ack chan struct{}) {
	c.closedMu.Lock()
	defer c.closedMu.Unlock()

	c.closed = true

	if ack != nil {
		close(ack)
	}
}

func (c *ClientConn) IsClosed() bool {
	c.closedMu.Lock()
	defer c.closedMu.Unlock()

	return c.closed
}

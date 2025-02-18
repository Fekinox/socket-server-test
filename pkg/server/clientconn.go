package server

import (
	"log"
	"time"

	"github.com/Fekinox/socket-server-test/pkg/message"
	"github.com/gorilla/websocket"
)

type ClientConn struct {
	conn     *websocket.Conn
	username string

	messages chan *AcknowledgedMessage
}

func (c *ClientConn) readPump() {
	defer c.conn.Close()

	c.conn.SetReadDeadline(time.Now().Add(PONG_WAIT_TIME))
	c.conn.SetPongHandler(func(appData string) error {
		log.Println("pong")
		c.conn.SetReadDeadline(time.Now().Add(PONG_WAIT_TIME))
		return nil
	})

	for {
		if _, _, err := c.conn.NextReader(); err != nil {
			break
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
		case ct := <-c.messages:
			err := c.conn.WriteControl(
				ct.Type,
				ct.Data,
				time.Now().Add(PING_PERIOD),
			)
			if err != nil {
				return
			}
			if ct.ack != nil {
				ct.ack <- err
				close(ct.ack)
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(WRITE_WAIT_TIME))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
			log.Println("ping")
		}
	}
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

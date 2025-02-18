package main

import (
	"time"

	"github.com/Fekinox/socket-server-test/pkg/message"
	"github.com/gorilla/websocket"
)

type ClientConn struct {
	conn     *websocket.Conn
	username string

	control chan *AcknowledgedMessage
}

func (c *ClientConn) readPump() {
	defer c.conn.Close()

	for {
		if _, _, err := c.conn.NextReader(); err != nil {
			break
		}
	}
}

func (c *ClientConn) writePump() {
	defer c.conn.Close()

	for {
		select {
		case ct := <-c.control:
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
		}
	}
}

func (c *ClientConn) WriteControl(typ int, data []byte) error {
	msg := AcknowledgedMessage{
		Message: message.Message{
			Type: typ,
			Data: data,
		},
		ack: make(chan error),
	}

	c.control <- &msg
	return <-msg.ack
}

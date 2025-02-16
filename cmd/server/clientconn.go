package main

import "github.com/gorilla/websocket"

type ClientConn struct {
	conn     *websocket.Conn
	username string
}

func (c *ClientConn) readPump() {
	defer c.conn.Close()

	for {
		if _, _, err := c.conn.NextReader(); err != nil {
			break
		}
	}
}

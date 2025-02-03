package main

import (
	"io"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize: 1024,
	WriteBufferSize:1024,
	CheckOrigin: func(r *http.Request) bool {return true},
}

type Message struct {
	Type int
	Data []byte
}

type Client struct {
	conn *websocket.Conn
	inboundMessages chan Message
	outboundMessages chan Message
}

func (c *Client) ReadLoop(sv *SocketServer) {
	defer func() {
		close(c.inboundMessages)
	}()

	for {
		typ, msg, err := c.conn.NextReader()
		if err != nil {
			c.conn.Close()
			break
		}

		data, err := io.ReadAll(msg)
		if err != nil {
			continue
		}

		c.inboundMessages <- Message{
			Type: typ,
			Data: data,
		}
	}
	log.Println("read done")
}

func (c *Client) WriteLoop(sv *SocketServer) {
	for {
		msg, ok := <-c.outboundMessages
		if !ok {
			c.conn.Close()
			break
		}

		processWrite := func() error {
			w, err := c.conn.NextWriter(msg.Type)
			if err != nil {
				return err
			}
			defer w.Close()

			if _, err := w.Write(msg.Data); err != nil {
				return err
			}
			return nil
		}

		if err := processWrite(); err != nil {
			c.conn.Close()
			break
		}
	}
	log.Println("write done")
}

type SocketServer struct {
}

func (s *SocketServer) EstablishConnection(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}

	c := Client{
		conn: conn,
		inboundMessages: make(chan Message, 256),
		outboundMessages: make(chan Message, 256),
	}

	go c.ReadLoop(s)
	go c.WriteLoop(s)

	for {
		m, ok := <- c.inboundMessages
		if !ok {
			break
		}

		if m.Type == websocket.TextMessage {
			log.Printf("text message: %s", m.Data)
		} else {
			log.Printf("binary message: %v", m.Data)
		}

		c.outboundMessages <- m
	}
}

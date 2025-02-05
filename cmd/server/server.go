package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"github.com/Fekinox/socket-server-test/pkg/message"
)

const (
	WRITE_WAIT_TIME = 10 * time.Second
	PONG_WAIT_TIME = 10 * time.Second
	PING_PERIOD = (PONG_WAIT_TIME * 9)/10
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

type SocketServer struct {
	clients   map[*ClientConn]struct{}

	shutdown chan struct{}
	register chan *ClientConn
	unregister chan *ClientConn

	messages chan ClientMessage
	broadcast chan message.Message

	TokenManager *TokenManager
}

func NewSocketServer() *SocketServer {
	return &SocketServer{
		clients: make(map[*ClientConn]struct{}),
		shutdown: make(chan struct{}),
		register: make(chan *ClientConn),
		unregister: make(chan *ClientConn),
		messages: make(chan ClientMessage),
		broadcast: make(chan message.Message),

		TokenManager: NewTokenManager(),
	}
}

type ClientMessage struct {
	message.Message
	Client *ClientConn
}

type ClientConn struct {
	conn             *websocket.Conn
	server           *SocketServer
	outboundMessages chan message.Message

	username string
}

func (s *SocketServer) Run() {
	for {
		select {
		case m, ok := <-s.messages:
			if !ok {
				break
			}
			msg := fmt.Sprintf("%s: %s", m.Client.username, m.Data)
			for c, _ := range s.clients {
				c.outboundMessages <- message.Message{
					Type: websocket.TextMessage,
					Data: []byte(msg),
				}
			}
		case <-s.shutdown:
			log.Println("Shutting down WebSocket server...")
			for c, _ := range s.clients {
				c.conn.Close()
				close(c.outboundMessages)
			}
			break
		case c := <-s.register:
			s.clients[c] = struct{}{}
			log.Printf("Registered new client (%v)", len(s.clients))

		case c := <-s.unregister:
			if _, ok := s.clients[c]; ok {
				delete(s.clients, c)
				close(c.outboundMessages)
				log.Printf("Unregistered client (%v)", len(s.clients))
			}
		}
	}
}

func (s *SocketServer) QueueShutdown() {
	s.shutdown <- struct{}{}
}

func (s *SocketServer) OpenClient(conn *websocket.Conn, username string) *ClientConn {
	c := &ClientConn{
		conn:             conn,
		server:           s,
		outboundMessages: make(chan message.Message, 256),
		username: username,
	}

	s.register <- c
	return c
}

func (c *ClientConn) Close() {
	c.server.unregister <- c
	c.conn.Close()
}

func (c *ClientConn) ReadLoop(sv *SocketServer) {
	defer c.Close()

	c.conn.SetReadDeadline(time.Now().Add(PONG_WAIT_TIME))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(PONG_WAIT_TIME))
		return nil
	})

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

		c.server.messages <- ClientMessage{
			Message: message.Message{
				Type: typ,
				Data: data,
			},
			Client: c,
		}
	}
	log.Println("read done")
}

func (c *ClientConn) WriteLoop(sv *SocketServer) {
	t := time.NewTicker(PING_PERIOD)
	defer func() {
		t.Stop()
		c.Close()
	}()

	for {
		select {
		case msg, ok := <-c.outboundMessages:
			c.conn.SetWriteDeadline(time.Now().Add(WRITE_WAIT_TIME))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				break
			}

			w, err := c.conn.NextWriter(msg.Type)
			if err != nil {
				return
			}

			w.Write(msg.Data)

			if err := w.Close(); err != nil {
				return
			}

		case <-t.C:
			c.conn.SetWriteDeadline(time.Now().Add(WRITE_WAIT_TIME))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (s *SocketServer) ServeWS(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		w.WriteHeader(http.StatusBadRequest)	
		return
	}

	payload, err := s.TokenManager.ValidateToken(token)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}

	c := s.OpenClient(conn, payload.Username)

	go c.ReadLoop(s)
	go c.WriteLoop(s)
}

func (s *SocketServer) CreateToken(w http.ResponseWriter, r *http.Request) {
	// Parse body
	var body struct{
		Username string `json:"username" required:"true"`
	}

	err := json.NewDecoder(r.Body).Decode(&body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if body.Username == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	token, err := s.TokenManager.GenerateToken(body.Username)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"token": token,
	})
}

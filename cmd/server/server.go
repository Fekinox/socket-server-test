package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/Fekinox/socket-server-test/pkg/message"
)

const (
	WRITE_WAIT_TIME = 10 * time.Second
	PONG_WAIT_TIME  = 60 * time.Second
	PING_PERIOD     = (PONG_WAIT_TIME * 9) / 10
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

type SocketServer struct {
	clients map[string]*ClientConn

	shutdown   chan struct{}
	register   chan *ClientConn
	unregister chan *ClientConn

	messages  chan ClientMessage
	broadcast chan message.Message

	TokenManager *TokenManager

	lobbies map[string]Lobby
}

type ClientMessage struct {
	message.Message
	Client *ClientConn
}

type ClientConn struct {
	conn             *websocket.Conn
	connected        bool
	server           *SocketServer
	outboundMessages chan message.Message

	username string
	lobby    string
}

type Lobby struct {
	name    string
	clients map[string]struct{}
	host    string
}

func NewSocketServer() *SocketServer {
	return &SocketServer{
		clients:    make(map[string]*ClientConn),
		shutdown:   make(chan struct{}),
		register:   make(chan *ClientConn),
		unregister: make(chan *ClientConn),
		messages:   make(chan ClientMessage),
		broadcast:  make(chan message.Message),

		TokenManager: NewTokenManager(),
		lobbies:      make(map[string]Lobby),
	}
}

func (s *SocketServer) Run() {
	for {
		select {
		case m, ok := <-s.messages:
			if !ok {
				break
			}

			tokens := strings.Fields(string(m.Data))
			switch tokens[0] {
			case "lobbies":
				for ln, lb := range s.lobbies {
					m.Client.outboundMessages <- message.Message{
						Type: websocket.TextMessage,
						Data: []byte(fmt.Sprintf("%s (%d)", ln, len(lb.clients))),
					}
				}
			case "status":
				m.Client.outboundMessages <- message.Message{
					Type: websocket.TextMessage,
					Data: []byte("not in any lobbies"),
				}
			case "new":
				s.RemoveClientFromTheirLobby(m.Client)
				lobbyName := s.CreateLobby()
				m.Client.outboundMessages <- message.Message{
					Type: websocket.TextMessage,
					Data: []byte(fmt.Sprintf("Created lobby %s", lobbyName)),
				}
				s.AddClientToLobby(m.Client, lobbyName)
			case "join":
				if len(tokens) < 2 {
					m.Client.outboundMessages <- message.Message{
						Type: websocket.TextMessage,
						Data: []byte("Must provide lobby"),
					}
				}
				lb, ok := s.lobbies[tokens[1]]
				if !ok {
					m.Client.outboundMessages <- message.Message{
						Type: websocket.TextMessage,
						Data: []byte(fmt.Sprintf("Lobby %s does not exist", tokens[1])),
					}
				}
				s.AddClientToLobby(m.Client, lb.name)

			}
		case <-s.shutdown:
			log.Println("Shutting down WebSocket server...")
			for _, c := range s.clients {
				c.conn.WriteMessage(
					websocket.CloseMessage,
					websocket.FormatCloseMessage(
						websocket.CloseGoingAway,
						"Shutting down",
					))
			}
			break
		case c := <-s.register:
			if _, ok := s.clients[c.username]; ok {
				continue
			}
			s.clients[c.username] = c
			c.connected = true
			log.Printf("Registered new client (%v)", len(s.clients))

		case c := <-s.unregister:
			if _, ok := s.clients[c.username]; ok {
				delete(s.clients, c.username)
				close(c.outboundMessages)
				log.Printf("Unregistered client (%v)", len(s.clients))
			}
		}
	}
}

func (s *SocketServer) QueueShutdown() {
	s.shutdown <- struct{}{}
}

func (s *SocketServer) OpenClientConn(conn *websocket.Conn, username string) {
	c := &ClientConn{
		conn:             conn,
		server:           s,
		outboundMessages: make(chan message.Message, 256),
		username:         username,
	}

	s.register <- c

	go c.ReadLoop(s)
	go c.WriteLoop(s)
}

func (c *ClientConn) Close() {
	c.server.unregister <- c
	c.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "Closed"))
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
			log.Printf("%v: %v", c, err)
			c.conn.Close()
			break
		}

		data, err := io.ReadAll(msg)
		if err != nil {
			log.Printf("%v: %v", c, err)
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
		return
	}

	s.OpenClientConn(conn, payload.Username)
}

func (s *SocketServer) CreateToken(w http.ResponseWriter, r *http.Request) {
	// Parse body
	var body struct {
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

// TODO: Decouple the raw WebSocket communications code from the higher-level lobby management code
func (s *SocketServer) CreateLobby() string {
	l := Lobby{
		clients: map[string]struct{}{},
	}

	name := ""
	for {
		if name != "" {
			if _, ok := s.lobbies[name]; !ok {
				break
			}
		}

		var sb strings.Builder
		for range 4 {
			sb.WriteRune(rune(rand.Intn(26)) + 'A')
		}
		name = sb.String()
	}

	l.name = name
	s.lobbies[name] = l

	return name
}

func (s *SocketServer) RemoveClientFromTheirLobby(c *ClientConn) {
	lb, ok := s.lobbies[c.lobby]
	if !ok {
		return
	}
	delete(lb.clients, c.username)
	if len(lb.clients) == 0 {
		delete(s.lobbies, lb.name)
		return
	} else if lb.host == c.username {
		for n, _ := range lb.clients {
			lb.host = n
			break
		}
	}
}

func (s *SocketServer) AddClientToLobby(c *ClientConn, l string) {
	lb, ok := s.lobbies[l]
	if !ok {
		return
	}
	if len(lb.clients) == 0 {
		lb.host = c.username
	}
	lb.clients[c.username] = struct{}{}
	c.lobby = l
}

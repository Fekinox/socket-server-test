package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"sync"
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
	clientConns map[string]*ClientConn

	isShuttingDown bool
	done           chan struct{}

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
	closed           bool
	closedMu         sync.Mutex
	conn             *websocket.Conn
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
		clientConns: make(map[string]*ClientConn),
		shutdown:    make(chan struct{}),
		register:    make(chan *ClientConn),
		unregister:  make(chan *ClientConn),
		messages:    make(chan ClientMessage),
		broadcast:   make(chan message.Message),

		TokenManager: NewTokenManager(),
		lobbies:      make(map[string]Lobby),
		done:         make(chan struct{}),
	}
}

func (s *SocketServer) Run() {
outer:
	for {
		if s.isShuttingDown && len(s.clientConns) == 0 {
			break outer
		} else {
			log.Println(len(s.clientConns))
		}
		select {
		case m, ok := <-s.messages:
			if !ok {
				break
			}

			s.HandleMessage(m)

		case c := <-s.register:
			if _, ok := s.clientConns[c.username]; ok {
				log.Printf("Client %v already exists", c.username)
				go c.Close()
				continue outer
			}
			s.clientConns[c.username] = c
			log.Printf("Registered new client (%v)", len(s.clientConns))

			go c.ReadLoop(s)
			go c.WriteLoop(s)

		case c := <-s.unregister:
			log.Println("unregistering", c)
			if v, ok := s.clientConns[c.username]; ok {
				if v != c {
					continue
				}

				delete(s.clientConns, c.username)
				close(c.outboundMessages)
				log.Println("unregister done")
				err := c.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "Closed"))
				if err != nil {
					c.conn.Close()
				}
				log.Printf("Unregistered client (%v)", len(s.clientConns))
			}

		case <-s.shutdown:
			log.Println("Shutting down WebSocket server...")
			s.isShuttingDown = true
			for _, c := range s.clientConns {
				log.Println("closing", c)
				go c.Close()
			}
			log.Println("Ordered all connections to close")
		}
	}
	close(s.done)
	log.Println("main loop done")
}

func (s *SocketServer) QueueShutdown() {
	s.shutdown <- struct{}{}
}

func (s *SocketServer) HandleMessage(m ClientMessage) {
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
}

func (s *SocketServer) OpenClientConn(conn *websocket.Conn, username string) {
	c := &ClientConn{
		conn:             conn,
		server:           s,
		outboundMessages: make(chan message.Message, 256),
		username:         username,
	}

	s.register <- c
}

func (c *ClientConn) Close() {
	log.Println("locking")
	c.closedMu.Lock()
	defer func() {
		log.Println("unlocking")
		c.closedMu.Unlock()
		log.Println("close done")
	}()

	if c.closed {
		return
	}

	c.server.unregister <- c
	c.closed = true
}

// Reader pump. Reads messages from the underlying WebSocket connection and forwards them to the server,
// while also recording the client who sent the message. Will halt once the underlying connection fails.
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
	log.Println("read closed")
}

// Writer pump. Receives messages from the server and forwards them to the underlying WebSocket connection.
// Will halt once the underlying connection fails, or once the server closes.
func (c *ClientConn) WriteLoop(sv *SocketServer) {
	t := time.NewTicker(PING_PERIOD)
	defer func() {
		t.Stop()
		c.Close()
	}()

outer:
	for {
		select {
		case msg, ok := <-c.outboundMessages:
			c.conn.SetWriteDeadline(time.Now().Add(WRITE_WAIT_TIME))
			if !ok {
				break outer
			}

			w, err := c.conn.NextWriter(msg.Type)
			if err != nil {
				break outer
			}

			w.Write(msg.Data)

			if err := w.Close(); err != nil {
				break outer
			}

		case <-t.C:
			c.conn.SetWriteDeadline(time.Now().Add(WRITE_WAIT_TIME))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				break outer
			}
		}
	}
	log.Println("write closed")
}

// Initiates a new WebSocket connection.
// Query parameters:
// * `token`: Authentication token received from the backend. If not present or invalid,
// rejects with a 400 error.
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

// Creates a token for use in initiating a WebSocket connection. Clients are expected to
// handshake by requesting a token from the backend and then using that token to initiate a
// connection.
func (s *SocketServer) CreateToken(w http.ResponseWriter, r *http.Request) {
	// Parse body
	var body struct {
		Username string `json:"username" required:"true"`
	}

	err := json.NewDecoder(r.Body).Decode(&body)
	if err != nil || body.Username == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	token, err := s.TokenManager.GenerateToken(body.Username)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
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

package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/Fekinox/socket-server-test/pkg/message"
	"github.com/gorilla/websocket"
)

const (
	WRITE_WAIT_TIME = 2 * time.Second
	PONG_WAIT_TIME  = 60 * time.Second
	PING_PERIOD     = (PONG_WAIT_TIME * 9) / 10
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

type AcknowledgedMessage struct {
	message.Message
	ack chan error
}

type SocketServer struct {
	TokenManager *TokenManager

	shutdown    chan struct{}
	hasShutdown chan struct{}
	sdOnce      sync.Once

	clients   map[*ClientConn]struct{}
	clientsMu sync.Mutex

	register   chan *ClientConn
	unregister chan *ClientConn
}

func NewSocketServer() *SocketServer {
	return &SocketServer{
		TokenManager: NewTokenManager(),
		shutdown:     make(chan struct{}),
		hasShutdown:  make(chan struct{}),

		clients: map[*ClientConn]struct{}{},

		register:   make(chan *ClientConn),
		unregister: make(chan *ClientConn),
	}
}

func (s *SocketServer) Run() {
outer:
	for {
		select {
		case cl := <-s.register:
			s.RegisterClient(cl)

		case cl := <-s.unregister:
			s.UnregisterClient(cl)

		case <-s.shutdown:
			s.DoShutdown()
			break outer
		}
	}
	log.Println("main loop done")
	close(s.hasShutdown)
}

func (s *SocketServer) Shutdown() {
	s.sdOnce.Do(func() {
		close(s.shutdown)
		<-s.hasShutdown
	})
}

func (s *SocketServer) RegisterClient(cl *ClientConn) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	s.clients[cl] = struct{}{}

	cl.conn.SetCloseHandler(func(code int, text string) error {
		log.Println("close handler", code, text)
		s.unregister <- cl

		cl.WriteCloseMessage(code, "received close message, goodbye")

		return nil
	})

	go cl.readPump()
	go cl.writePump()

	cl.On("ping", func(username, body string) {
		cl.WriteTextMessage(body)
		cl.WriteTextMessage("pong")
	})

	log.Println("Registered new client", cl.Username, len(s.clients))

	if err := cl.WriteTextMessage("hello"); err != nil {
		log.Println(err)
	}
}

func (s *SocketServer) UnregisterClient(cl *ClientConn) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()

	delete(s.clients, cl)
	log.Println("Unregistered client ", cl.Username)
}

func (s *SocketServer) DoShutdown() {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()

	for cl := range s.clients {
		log.Println("closing", cl.Username)
		cl.WriteCloseMessage(websocket.CloseNormalClosure, "goodbye")
		log.Println("close done")
	}
}

// Initiates a new WebSocket connection.
//
// Query parameters:
//
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

	var _, _ = payload, conn

	s.register <- &ClientConn{
		conn:     conn,
		Username: payload.Username,

		messages: make(chan *AcknowledgedMessage),

		handlers: make(map[string]HandlerFunc),
	}
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

	err = func() error {
		s.clientsMu.Lock()
		defer s.clientsMu.Unlock()

		for cl := range s.clients {
			if cl.Username == body.Username {
				return fmt.Errorf("user %s already exists", body.Username)
			}
		}
		return nil
	}()

	if err != nil {
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

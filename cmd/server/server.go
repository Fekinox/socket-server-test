package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
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
			func() {
				s.clientsMu.Lock()
				defer s.clientsMu.Unlock()
				s.clients[cl] = struct{}{}

				cl.conn.SetCloseHandler(func(code int, text string) error {
					log.Println(code, text)
					s.unregister <- cl
					message := websocket.FormatCloseMessage(code, "")

					cl.conn.WriteControl(
						websocket.CloseMessage,
						message,
						time.Now().Add(PING_PERIOD),
					)
					return nil
				})

				go cl.readPump()

				go func() {
					// s.unregister <- cl

					// cl.conn.WriteMessage(
					// 	websocket.CloseMessage,
					// 	websocket.FormatCloseMessage(
					// 		websocket.CloseTryAgainLater,
					// 		"Try again later",
					// 	),
					// )
				}()
				log.Println("Registered new client", cl.username, len(s.clients))
			}()

		case cl := <-s.unregister:
			func() {
				s.clientsMu.Lock()
				defer s.clientsMu.Unlock()

				delete(s.clients, cl)
				log.Println("Unregistered client ", cl.username)
			}()

		case <-s.shutdown:
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
		username: payload.Username,
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

		for cl, _ := range s.clients {
			if cl.username == body.Username {
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

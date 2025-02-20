package server

import (
	"encoding/json"
	"fmt"
	"log"
	"maps"
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

type ClientTag struct {
	username string
	tag      string
}

type SocketServer struct {
	TokenManager *TokenManager

	shutdown    chan struct{}
	hasShutdown chan struct{}
	sdOnce      sync.Once

	clients     map[*ClientConn]struct{}
	clientsMu   sync.Mutex
	usernameMap map[string]*ClientConn
	clientTags  map[ClientTag]struct{}

	register   chan *ClientConn
	unregister chan *ClientConn

	handleConnect    func(cl *ClientConn)
	handleDisconnect func(cl *ClientConn)
}

func NewSocketServer() *SocketServer {
	s := &SocketServer{
		TokenManager: NewTokenManager(),
		shutdown:     make(chan struct{}),
		hasShutdown:  make(chan struct{}),

		clients:     map[*ClientConn]struct{}{},
		usernameMap: make(map[string]*ClientConn),
		clientTags:  make(map[ClientTag]struct{}),

		register:   make(chan *ClientConn),
		unregister: make(chan *ClientConn),
	}

	s.SetConnectHandler(nil)
	s.SetDisconnectHandler(nil)

	return s
}

func (s *SocketServer) Run() {
outer:
	for {
		select {
		case cl := <-s.register:
			s.RegisterClient(cl)
			s.handleConnect(cl)

		case cl := <-s.unregister:
			s.UnregisterClient(cl)
			s.handleDisconnect(cl)

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
	s.usernameMap[cl.Username] = cl

	cl.conn.SetCloseHandler(func(code int, text string) error {
		log.Println("close handler", code, text)
		if err := cl.WriteCloseMessage(code, "received close message, goodbye"); err != nil {
			cl.conn.Close()
		}
		s.unregister <- cl

		return nil
	})

	go cl.readPump()
	go cl.writePump()

	log.Println("Registered new client", cl.Username, len(s.clients))
}

func (s *SocketServer) UnregisterClient(cl *ClientConn) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()

	delete(s.clients, cl)
	delete(s.usernameMap, cl.Username)
	close(cl.messages)

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

func (s *SocketServer) SetConnectHandler(h func(cl *ClientConn)) {
	if h == nil {
		h = func(*ClientConn) {}
	}
	s.handleConnect = h
}

func (s *SocketServer) SetDisconnectHandler(h func(cl *ClientConn)) {
	if h == nil {
		h = func(*ClientConn) {}
	}
	s.handleDisconnect = h
}

func (s *SocketServer) GetClientByUsername(username string) (*ClientConn, bool) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()

	cl, ok := s.usernameMap[username]
	if !ok {
		return nil, false
	}
	if _, ok := s.clients[cl]; !ok {
		return nil, false
	}

	return cl, ok
}

func (s *SocketServer) Usernames(tags ...string) []string {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()

	var names []string
	for u, cl := range s.usernameMap {
		if _, ok := s.clients[cl]; !ok {
			continue
		}

		if len(tags) != 0 {
			var found bool
			for _, t := range tags {
				_, ok := s.clientTags[ClientTag{
					username: u,
					tag:      t,
				}]
				if ok {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		names = append(names, u)
	}

	return names
}

func (s *SocketServer) AddClientToTag(u, t string) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()

	ct := ClientTag{
		username: u,
		tag:      t,
	}
	s.clientTags[ct] = struct{}{}
}

func (s *SocketServer) RemoveAllTagsFromClient(u string) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()

	maps.DeleteFunc(s.clientTags, func(ct ClientTag, _ struct{}) bool {
		return ct.username == u
	})
}

func (s *SocketServer) RemoveClientFromTag(u, t string) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()

	delete(s.clientTags, ClientTag{
		username: u,
		tag:      t,
	})
}

func (s *SocketServer) ClientHasTag(u, t string) bool {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()

	_, ok := s.clientTags[ClientTag{
		username: u,
		tag:      t,
	}]

	return ok
}

func (s *SocketServer) GetClientTags(u string) []string {
	var tags []string
	for ct := range s.clientTags {
		if ct.username == u {
			tags = append(tags, ct.tag)
		}
	}

	return tags
}

func (s *SocketServer) Broadcast(typ int, data []byte, users ...string) {
	for _, u := range users {
		cl, ok := s.GetClientByUsername(u)
		if !ok {
			continue
		}
		log.Println(u)
		if cl.IsClosed() {
			continue
		}
		cl.WriteMessage(typ, data)
	}
}

func (s *SocketServer) BroadcastText(data string, users ...string) {
	s.Broadcast(websocket.TextMessage, []byte(data), users...)
}

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/Fekinox/socket-server-test/pkg/message"
	"github.com/gorilla/websocket"
)

var (
	ErrTooManyReconnectAttempts = errors.New("Too many reconnection attempts")
	ErrServerDead               = errors.New("Server is dead, cannot reconnect")
	ErrQuit                     = errors.New("Client quit")
	ErrReconnectionInterrupted  = errors.New("Reconnection interrupted by OS signal")
)

type ConnectionState int

const (
	MAX_BACKOFF_TIME_MS      = 5 * 1000
	INIT_BACKOFF_TIME_MS     = 250
	BACKOFF_TIME_MULT_FACTOR = 2
	RANDOM_BACKOFF_MAX       = 1000
	MAX_RETRY_ATTEMPTS       = 20
)

const (
	Disconnected ConnectionState = iota
	Connected
	ServerDead
	ClientQuit
)

type Client struct {
	Host     string
	Username string

	conn *websocket.Conn

	connected   ConnectionState
	connectedMu sync.Mutex

	inboundMessages  chan message.Message
	outboundMessages chan message.Message
	errors           chan error
	done             chan struct{}
}

func (c *Client) Run() error {
	if err := c.EnsureConnected(); err != nil {
		return err
	}

	go c.ReadLoop()
	go c.WriteLoop()

	c.conn.SetCloseHandler(func(code int, text string) error {
		c.connectedMu.Lock()
		defer c.connectedMu.Unlock()

		c.conn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(code, text),
		)

		switch code {
		case websocket.CloseGoingAway, websocket.CloseAbnormalClosure:
			c.connected = ServerDead
		default:
			c.connected = Disconnected
		}

		err := &websocket.CloseError{
			Code: code,
			Text: text,
		}

		c.errors <- err

		return err
	})

	for {
		select {
		case im, ok := <-c.inboundMessages:
			_, _ = im, ok
		case err := <-c.errors:
			close(c.outboundMessages)
			close(c.done)
			return err
		}
	}
}

func (c *Client) Quit() {
	c.connectedMu.Lock()
	defer c.connectedMu.Unlock()

	if c.connected == Connected {
		err := c.conn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseGoingAway, ""),
		)
		if err != nil {
			log.Println("write close: ", err)
			c.conn.Close()
			return
		}
	}

	close(c.done)
	c.connected = ClientQuit
}

func (c *Client) ReadLoop() {
	for {
		if err := c.EnsureConnected(); err != nil {
			return
		}

		for {
			typ, msg, err := c.conn.ReadMessage()
			if err != nil {
				break
			}

			log.Printf("%s", msg)

			c.inboundMessages <- message.Message{
				Type: typ,
				Data: msg,
			}
		}
	}
}

func (c *Client) WriteLoop() {
	for {
		if err := c.EnsureConnected(); err != nil {
			return
		}

		for {
			msg, ok := <-c.outboundMessages
			if !ok {
				break
			}

			w, err := c.conn.NextWriter(msg.Type)
			if err != nil {
				break
			}

			w.Write(msg.Data)

			if err := w.Close(); err != nil {
				break
			}
		}
	}
}

func (c *Client) EnsureConnected() error {
	c.connectedMu.Lock()
	defer c.connectedMu.Unlock()

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, os.Kill, syscall.SIGTERM, syscall.SIGINT)

	switch c.connected {
	case Connected:
		return nil
	case ServerDead:
		return ErrServerDead
	case ClientQuit:
		return ErrQuit
	}

	var err error

	var retryTime = INIT_BACKOFF_TIME_MS

	for range MAX_RETRY_ATTEMPTS {
		log.Println("reconnecting...", retryTime)
		randomTime := rand.Intn(RANDOM_BACKOFF_MAX)
		timeout := time.After(time.Duration(retryTime+randomTime) * time.Millisecond)
		err = c.connect()
		if err == nil {
			log.Println("connected")
			c.connected = Connected
			return nil
		}
		select {
		case <-timeout:
			retryTime = min(MAX_BACKOFF_TIME_MS, retryTime*BACKOFF_TIME_MULT_FACTOR)
		case <-interrupt:
			return ErrReconnectionInterrupted
		}
	}
	return ErrTooManyReconnectAttempts
}

func (c *Client) connect() error {
	// Generate a token first
	var buf bytes.Buffer
	err := json.NewEncoder(&buf).Encode(map[string]any{
		"username": c.Username,
	})
	if err != nil {
		return err
	}

	tokenUrl := url.URL{
		Scheme: "http",
		Host:   c.Host,
		Path:   "/create-token",
	}

	resp, err := http.Post(tokenUrl.String(), "application/json", &buf)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusCreated {
		return errors.New(resp.Status)
	}

	var token struct {
		Token string `json:"token" required:"true"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return err
	}

	u := url.URL{
		Scheme: "ws",
		Host:   c.Host,
		Path:   "/ws",
	}
	q := u.Query()
	q.Set("token", token.Token)
	u.RawQuery = q.Encode()

	c.conn, _, err = websocket.DefaultDialer.Dial(u.String(), nil)
	return err
}

func (c *Client) ReadMessage() (message.Message, error) {
	typ, msg, err := c.conn.ReadMessage()
	if err != nil {
		return message.Message{}, err
	}
	return message.Message{
		Type: typ,
		Data: msg,
	}, nil
}

func (c *Client) WriteMessage(m message.Message) {
	c.outboundMessages <- m
}

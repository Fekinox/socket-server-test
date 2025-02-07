package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/Fekinox/socket-server-test/pkg/message"
	"github.com/gorilla/websocket"
)

type Client struct {
	Host     string
	Username string

	conn      *websocket.Conn
	connected bool

	inboundMessages  chan message.Message
	outboundMessages chan message.Message
}

func (c *Client) Run() error {
	return nil
}

func (c *Client) Connect() error {
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
	if err != nil || resp.StatusCode != http.StatusOK {
		return err
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

func (c *Client) WriteMessage(m message.Message) error {
	return c.conn.WriteMessage(m.Type, m.Data)
}

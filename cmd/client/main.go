package main

import (
	"bufio"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Fekinox/socket-server-test/pkg/message"
	"github.com/gorilla/websocket"
)

var username = flag.String("username", "foobar", "username")

func main() {
	flag.Parse()
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, os.Kill, syscall.SIGTERM, syscall.SIGINT)

	cl := Client{
		Host:     "localhost:3000",
		Username: *username,
	}
	err := cl.Connect()
	if err != nil {
		log.Fatal(err)
	}
	defer cl.conn.Close()

	done := make(chan struct{})

	lines := make(chan string, 256)

	go func() {
		defer close(done)
		for {
			msg, err := cl.ReadMessage()
			if err != nil {
				log.Println("read:", err)
				return
			}
			log.Printf("recv: %s", msg.Data)
		}
	}()

	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()

	for {
		select {
		case ln, ok := <-lines:
			if !ok {
				return
			}
			err := cl.WriteMessage(message.Message{
				Type: websocket.TextMessage,
				Data: []byte(ln),
			})
			if err != nil {
				return
			}
		case <-done:
			return
		case <-interrupt:
			err := cl.conn.WriteMessage(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			if err != nil {
				log.Println("write close: ", err)
				return
			}

			select {
			case <-done:
			case <-time.After(time.Second):
			}

			return
		default:
		}
	}
}

package main

import (
	"bufio"
	"flag"
	"os"
	"os/signal"
	"syscall"

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

		inboundMessages:  make(chan message.Message, 256),
		outboundMessages: make(chan message.Message, 256),
		errors:           make(chan error, 256),
		done:             make(chan struct{}),
	}

	go cl.Run()
	defer cl.Quit()

	lines := make(chan string, 256)

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
			cl.WriteMessage(message.Message{
				Type: websocket.TextMessage,
				Data: []byte(ln),
			})
		case <-interrupt:
			cl.Quit()

			return
		case <-cl.done:
			return
		}
	}
}

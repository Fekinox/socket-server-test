package main

import (
	"bufio"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/Fekinox/socket-server-test/pkg/client"
	"github.com/Fekinox/socket-server-test/pkg/message"
	"github.com/gorilla/websocket"
)

var username = flag.String("username", "foobar", "username")
var host = flag.String("host", "localhost", "host")
var port = flag.Int("port", 3000, "port")

func main() {
	flag.Parse()
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, os.Kill, syscall.SIGTERM, syscall.SIGINT)

	cl := client.NewClient(*host, *port, *username)

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
		case <-cl.Done():
			return
		}
	}
}

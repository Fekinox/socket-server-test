package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"

	// "runtime"
	"syscall"
	"time"

	"github.com/Fekinox/socket-server-test/pkg/server"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

func main() {
	gm := server.NewGameManager()

	ws := server.NewSocketServer()

	ws.SetConnectHandler(func(cl *server.ClientConn) {
		cl.On("ping", func(username, body string) {
			cl.WriteTextMessage(fmt.Sprintf("pong: %q", body))
		})

		cl.On("new", func(username, body string) {
			gm.NewLobby(ws, cl.Username)
		})

		cl.On("join", func(username, body string) {
			gm.JoinLobby(ws, cl.Username, body)
		})

		cl.On("leave", func(username, body string) {
			gm.RemoveFromLobby(ws, cl.Username)
		})

		cl.On("say", func(username, body string) {
			gm.SayInLobby(ws, username, body)
		})

		cl.On("info", func(username, body string) {
			gm.LobbyInfo(ws, username)
		})

		cl.On("start", func(username, body string) {
			gm.StartGame(ws, username)
		})

		cl.On("mark", func(username, body string) {
			tokens := strings.Fields(body)
			if len(tokens) < 2 {
				cl.WriteTextMessage("Must provide at least two arguments")
				return
			}

			x, err := strconv.Atoi(tokens[0])
			if err != nil {
				cl.WriteTextMessage("First argument must be an integer")
				return
			}
			y, err := strconv.Atoi(tokens[1])
			if err != nil {
				cl.WriteTextMessage("Second argument must be an integer")
				return
			}

			gm.Move(ws, username, server.Mark{X: x, Y: y})
		})

		cl.On("expand", func(username, body string) {
			var exp server.ExpandDirection
			switch body {
			case "up":
				exp = server.ExpandUp
			case "down":
				exp = server.ExpandDown
			case "left":
				exp = server.ExpandLeft
			case "right":
				exp = server.ExpandRight
			default:
				cl.WriteTextMessage("Argument to expand must be 'up', 'down', 'left', or 'right'")
				return
			}

			gm.Move(ws, username, server.Expand(exp))
		})

		cl.On("gamestate", func(username, body string) {
			gm.GetCurrentGameState(ws, username)
		})
	})

	ws.SetDisconnectHandler(func(cl *server.ClientConn) {
		gm.RemoveFromLobby(ws, cl.Username)
	})

	go ws.Run()

	// TODO: make the websocket server simply return an http servemux that can be mounted on a
	// subroute, kinda like what socket.io does
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:5173", "https://localhost:5173"},
		AllowCredentials: true,
	}))
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("welcome"))
	})

	r.Get("/ws", ws.ServeWS)

	r.Post("/create-token", ws.CreateToken)

	addr := fmt.Sprintf(":%v", 3000)

	srv := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %s\n", err)
		}
		log.Println("server done")
	}()

	// Wait for interrupt to gracefully shut down the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	fmt.Println("Shutting down...")
	ws.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Forced to shutdown: ", err)
	}

	fmt.Println("Successfully exited")
	// close(done)
}

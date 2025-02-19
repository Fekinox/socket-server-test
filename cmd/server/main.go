package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"

	// "runtime"
	"syscall"
	"time"

	"github.com/Fekinox/socket-server-test/pkg/server"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	ws := server.NewSocketServer()

	ws.SetConnectHandler(func(cl *server.ClientConn) {
		cl.On("ping", func(username, body string) {
			cl.WriteTextMessage(fmt.Sprintf("pong: %q", body))
		})

		if err := cl.WriteTextMessage("hello"); err != nil {
			log.Println(err)
		}
	})

	go ws.Run()

	r := chi.NewRouter()
	r.Use(middleware.Logger)
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

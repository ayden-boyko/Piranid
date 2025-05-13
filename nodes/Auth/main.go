package auth

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	node "github.com/ayden-boyko/Piranid/internal/node"
)

// Code for Auth node
func main() {
	// Create a new HTTP server. This server will be responsible for running the
	// API and handling requests.
	server := node.NewNode()

	// Run the server in a separate goroutine. This allows the server to run
	// concurrently with the other code.
	go func() {
		// Run the server and check for errors. This will block until the server
		// is shutdown.
		if err := server.Run(fmt.Sprintf(":%s", os.Getenv("AUTH_PORT")), "DB"); !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Error running Auth Node: %v", err)
		}
	}()

	// Create a channel to receive signals. This will allow us to gracefully
	// shutdown the server when it receives a SIGINT or SIGTERM.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for a signal to be received.
	<-sigChan

	// Create a context with a timeout to shut down the server.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	// Shutdown the server. This will block until the server is shutdown.
	if err := server.SafeShutdown(shutdownCtx); err != nil {
		log.Fatalf("\n Auth Node shutdown failed: %v", err)
	}
	log.Println("\n Auth Node shutdown safely completed")
}

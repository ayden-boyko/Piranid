package main

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

	node "Piranid/node"
	utils "Piranid/pkg"

	core "github.com/ayden-boyko/Piranid/nodes/Event_Queue/eventcore"

	_ "modernc.org/sqlite"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Code for Auth node
func main() {
	// Create a new HTTP server. This server will be responsible for sending
	// notifications
	server := &core.EventNode{Node: node.NewNode(), Service_ID: utils.NewServiceID("EVNT")}

	fmt.Println("Event Node created...")

	fmt.Println("Dialing Message Queue...")
	// TODO CHANGE THIS, use the right route
	conn, err := amqp.Dial("amqp://guest:guest@localhost:5672/")
	if err != nil {
		log.Panicf("%s: %s", "Failed to connect to RabbitMQ", err)
	}

	defer conn.Close()

	// Register Routes
	server.RegisterRoutes(conn)

	// Run the server in a separate goroutine. This allows the server to run
	// concurrently with the other code.
	go func() {
		// Run the server and check for errors. This will block until the server
		// is shutdown.
		fmt.Println("Starting Event Node...")
		if err := server.Run(fmt.Sprintf(":%s", os.Getenv("EVENT_QUEUE_PORT")), func() {}); !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Error running Event Node: %v", err)
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

	// TODO, look into if queues and channels need to be safely shutdown, and if so, how
	// Shutdown the server. This will block until the server is shutdown.
	if err := server.SafeShutdown(shutdownCtx); err != nil {
		log.Fatalf("\n Event Node shutdown failed: %v", err)
	}
	log.Println("\n Event Node shutdown safely completed")
}

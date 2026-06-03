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
	telemetry "Piranid/pkg/telemetry"

	core "github.com/ayden-boyko/Piranid/nodes/Event_Queue/eventcore"

	_ "modernc.org/sqlite"

	amqp "github.com/rabbitmq/amqp091-go"
)

// TODO: add ssl certs

// Code for Event node
func main() {

	ctx := context.Background()

	fmt.Println("Creating a new Event Node...")
	// Create a new HTTP server. This server will be responsible for sending
	// notifications
	server := &core.EventNode{Node: node.NewNode(), Service_ID: utils.NewServiceID("EVNT")}

	// telemetry setup
	// Set up telemetry
	collectorAddr := os.Getenv("OTEL_COLLECTOR_ADDR")
	if collectorAddr == "" {
		collectorAddr = "localhost:4317"
	}
	otelShutdown, err := telemetry.SetupOTelSDK(ctx, "Auth Node", collectorAddr)
	if err != nil {
		log.Fatalf("failed to set up telemetry: %v", err)
	}
	defer otelShutdown(ctx)

	// Set up logging
	logger, err := telemetry.NewLogger("notifications")
	if err != nil {
		log.Fatalf("failed to setup logger: %v", err)
	}
	defer logger.Sync()

	fmt.Println("Event Node created...")

	// get the port for the message queue from the environment variable, and connect to it
	MQ_PORT := os.Getenv("RABBIT_MQ_PORT")
	if MQ_PORT == "" {
		log.Panic("RABBIT_MQ_PORT environment variable not set")
	}

	fmt.Println("Dialing Message Queue...")
	conn, err := amqp.Dial(fmt.Sprintf("amqp://guest:guest@rabbitmq:%s/", MQ_PORT))
	if err != nil {
		log.Panicf("%s: %s", "Failed to connect to RabbitMQ", err)
	}

	defer conn.Close()

	// Run the server in a separate goroutine. This allows the server to run
	// concurrently with the other code.
	go func() {
		// Run the server and check for errors. This will block until the server
		// is shutdown.
		fmt.Println("Starting Event Node...")
		if err := server.Run(fmt.Sprintf(":%s", os.Getenv("EVENT_QUEUE_PORT")), func() {
			server.RegisterRoutes(conn, ctx, logger) // Pass the connection to the handlers
		}); !errors.Is(err, http.ErrServerClosed) {
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
	shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 10*time.Second)
	defer shutdownCancel()

	// Shutdown the server. This will block until the server is shutdown.
	if err := server.SafeShutdown(shutdownCtx); err != nil {
		log.Fatalf("\n Event Node shutdown failed: %v", err)
	}
	log.Println("\n Event Node shutdown safely completed")
}

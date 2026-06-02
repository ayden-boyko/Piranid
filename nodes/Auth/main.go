package main

import (
	"context"
	"embed"
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

	core "github.com/ayden-boyko/Piranid/nodes/Auth/authcore"

	"github.com/redis/go-redis/v9"
	_ "modernc.org/sqlite"
)

// TODO: add ssl certs

//go:embed templates/*
var TemplatesFS embed.FS

// Code for Auth node
func main() {

	ctx := context.Background()

	fmt.Println("Creating a new Auth Node...")

	// Create a new HTTP server. This server will be responsible for sending
	// notifications
	server := &core.AuthNode{Node: node.NewNode(), Service_ID: utils.NewServiceID("AUTH")}

	fmt.Println("Auth Node created...")

	err := utils.SetUpDB(server.Node, "sqlite", "sqlite://user:pass@192.168.x.x:5432/auth_db", "schema.sql")
	if err != nil {
		log.Fatalf("Error setting up DB: %v", err)
	}

	// Create a new Redis client
	redisClient := redis.NewClient(&redis.Options{
		Addr:     os.Getenv("REDIS_HOST") + ":" + os.Getenv("REDIS_PORT"),
		Password: "", // no password set
		DB:       0,  // use default DB
	})
	server.Node.SetCache(redisClient)

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

	// Run the server in a separate goroutine. This allows the server to run
	// concurrently with the other code.
	go func() {
		// Run the server and check for errors. This will block until the server
		// is shutdown.
		fmt.Println("Starting Auth Node...")
		if err := server.Run(fmt.Sprintf(":%s", os.Getenv("AUTH_PORT")), func() {
			server.RegisterRoutes(TemplatesFS, ctx) // TemplatesFS is captured here
		}); !errors.Is(err, http.ErrServerClosed) {
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
	shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 10*time.Second)
	defer shutdownCancel()

	// Shutdown the server. This will block until the server is shutdown.
	if err := server.SafeShutdown(shutdownCtx); err != nil {
		log.Fatalf("\n Auth Node shutdown failed: %v", err)
	}
	log.Println("\n Auth Node shutdown safely completed")
}

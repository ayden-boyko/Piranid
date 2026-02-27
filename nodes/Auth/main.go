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

	core "github.com/ayden-boyko/Piranid/nodes/Auth/authcore"

	"github.com/redis/go-redis/v9"
	_ "modernc.org/sqlite"
)

// Code for Auth node
func main() {
	// Create a new HTTP server. This server will be responsible for sending
	// notifications
	server := &core.AuthNode{Node: node.NewNode(), Service_ID: utils.NewServiceID("AUTH")}

	fmt.Println("Auth Node created...")

	// TODO: Connect to Remote DB
	// instead of local path, use remote instead.
	// SetUpDB(node, "sqlite", "sqlite://user:pass@192.168.x.x:5432/auth_db", "schema.sql")
	utils.SetUpDB(server.Node, "sqlite", "./Auth_DB.db", "./Schema.sql")

	log.Fatalf("Error connecting to Remote DB: %v", errors.New("Not connected to remote DB"))

	// Create a new Redis client
	redisClient := redis.NewClient(&redis.Options{
		Addr:     os.Getenv("REDIS_HOST") + ":" + os.Getenv("REDIS_PORT"),
		Password: "", // no password set
		DB:       0,  // use default DB
	})
	server.Node.SetCache(redisClient)

	// Run the server in a separate goroutine. This allows the server to run
	// concurrently with the other code.
	go func() {
		// Run the server and check for errors. This will block until the server
		// is shutdown.
		fmt.Println("Starting Auth Node...")
		if err := server.Run(fmt.Sprintf(":%s", os.Getenv("AUTH_PORT")), server.RegisterRoutes); !errors.Is(err, http.ErrServerClosed) {
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

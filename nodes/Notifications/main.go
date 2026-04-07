package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	node "Piranid/node"
	utils "Piranid/pkg"

	v1 "Piranid/pkg/proto/notifications/v1"

	"github.com/ayden-boyko/Piranid/nodes/Notifications/handlers"
	core "github.com/ayden-boyko/Piranid/nodes/Notifications/notifcore"

	"google.golang.org/grpc"

	"github.com/trycourier/courier-go/v2"
	_ "modernc.org/sqlite"
)

// TODO: add ssl certs

// Code for Notif node
func main() {
	fmt.Println("Creating a new Notification Node...")
	client := courier.CreateClient(
		os.Getenv("COURIER_TOKEN"), nil,
	)
	// Create a new HTTP server. This server will be responsible for sending
	// notifications
	server := &core.NotificationNode{Node: node.NewNode(), Messager: client, Service_ID: utils.NewServiceID("NOTF")}

	fmt.Println("Notification Node created...")

	notifHandler := handlers.NewNotificationHandler(server)

	grpcServer := grpc.NewServer()
	v1.RegisterNotifierServer(grpcServer, notifHandler)
	
	port := os.Getenv("NOTIFICATION_PORT")
	if port == "" {
		port = "8084"
	}

	listener, err := net.Listen("tcp", ":" + port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// create sqlite DB, run schema
	err = utils.SetUpDB(server.Node, "sqlite", "./Notification_DB.db", "./Schema.sql")
	if err != nil {
		log.Fatalf("failed to set up DB: %v", err)
	}

	// Run the server in a separate goroutine. This allows the server to run
	// concurrently with the other code.
	go func() {
		// Run the server and check for errors. This will block until the server
		// is shutdown.
		fmt.Println("Starting Notification Node...")
		if err := grpcServer.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Error running Notification Node: %v", err)
		}
	}()

	ctx := context.Background()

	// Start the message queue listener in a separate goroutine. This will allow
	// the server to listen for messages while still being able to shut down
	// gracefully.
	go server.StartMQListener(ctx)

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
		log.Fatalf("\n Notification Node shutdown failed: %v", err)
	}
	log.Println("\n Notification Node shutdown safely completed")
}

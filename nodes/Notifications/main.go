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

	node "github.com/ayden-boyko/Piranid/internal/node"
	"github.com/trycourier/courier-go/v2"
)

type NotificationNode struct {
	*node.Node
	Notifier *courier.Client
}

func (n *NotificationNode) RegisterRoutes() {
	// TODO Actual route registration for logging server
	n.Node.Router.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("Sending notification...")
		fmt.Fprint(w, "Sending notification...")
		client := courier.CreateClient(
			os.Getenv("COURIER_TOLKEN"), nil,
		)
		requestID, err := client.SendMessage(
			context.Background(),
			courier.SendMessageRequestBody{
				Message: map[string]interface{}{
					"to": map[string]string{
						"email": "aydenboyko@gmail.com",
					},
					"template": "2GPARRPY3S4WHDKV8G07V5JKNNHZ",
					"data": map[string]string{
						"data": "HAHAHA THIS IS TESTING DTATA",
					},
				},
			},
		)

		if err != nil {
			log.Fatalln(err)
		}
		log.Println(requestID)
		fmt.Println("Notification sent...")
	})

}

// Code for Auth node
func main() {
	fmt.Println("Creating a new Notification Node...")
	client := courier.CreateClient(
		os.Getenv("COURIER_TOLKEN"), nil,
	)
	// Create a new HTTP server. This server will be responsible for sending
	// notifications
	server := &NotificationNode{Node: node.NewNode(), Notifier: client}

	fmt.Println("Notification Node created...")

	// Run the server in a separate goroutine. This allows the server to run
	// concurrently with the other code.
	go func() {
		// Run the server and check for errors. This will block until the server
		// is shutdown.
		fmt.Println("Starting Notification Node...")
		if err := server.Run(fmt.Sprintf(":%s", os.Getenv("NOTIFICATION_PORT")), server.RegisterRoutes); !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Error running Notification Node: %v", err)
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
		log.Fatalf("\n Notification Node shutdown failed: %v", err)
	}
	log.Println("\n Notification Node shutdown safely completed")
}

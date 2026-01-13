package main

import (
	"context"
	"database/sql"
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

	core "github.com/ayden-boyko/Piranid/nodes/Notifications/notifcore"

	"github.com/trycourier/courier-go/v2"
	_ "modernc.org/sqlite"
)

// Code for Auth node
func main() {
	fmt.Println("Creating a new Notification Node...")
	client := courier.CreateClient(
		os.Getenv("COURIER_TOKEN"), nil,
	)
	// Create a new HTTP server. This server will be responsible for sending
	// notifications
	server := &core.NotificationNode{Node: node.NewNode(), Notifier: client, Service_ID: utils.NewServiceID("NOTI")}

	fmt.Println("Notification Node created...")
	fmt.Println("Initializing database...")

	db, err := sql.Open("sqlite", "./Notification_DB.db")
	if err != nil {
		log.Fatalf("Error opening database: %v", err)
	}
	fmt.Println("Database initialized...")

	server.Node.SetDB(db)

	// Read the contents of the initfile
	sqlScript, err := os.ReadFile("./Schema.sql")
	if err != nil {
		log.Fatalf("Error reading SQL script: %v", err)
	}

	// Execute the SQL script to initialize the database
	db, ok := server.Node.GetDB().(*sql.DB)
	if !ok {
		log.Fatalf("Error, expected server.Node.GetDB() to be of type *sql.DB, but got %T", server.Node.GetDB())
	}
	_, err = db.Exec(string(sqlScript))
	if err != nil {
		log.Fatalf("Error executing SQL script: %v, error within %s", err, string(sqlScript))
	}
	fmt.Println("Query executed...")

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

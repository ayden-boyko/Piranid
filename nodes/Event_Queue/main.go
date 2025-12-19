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

	_ "modernc.org/sqlite"
)

type EventNode struct {
	*node.Node
	service_ID string
}

//TODO Your service could listen for special `control` or `configuration events on a management queue or exchange.
// When it receives an event describing a new topic and binding, it would create them in RabbitMQ accordingly

//! SEPARATE QUEUE FOR 2FA

func (n *EventNode) GetServiceID() string { return n.service_ID }

func (n *EventNode) RegisterRoutes() {
	// TODO Actual route registration for logging server
	n.Node.Router.HandleFunc("/event_test", func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("Event recieved...")
		fmt.Fprint(w, "recieved")
	})

}

func (l *EventNode) ShutdownDB() error {
	db := l.Node.GetDB()
	if sqliteDB, ok := db.(*sql.DB); ok {
		sqliteDB.Close()
		fmt.Println("Database closed...")
		return nil
	}
	return errors.New("database is not *sql.DB, is type " + fmt.Sprintf("%T", db))
}

// SafeShutdown is a function that gracefully stops the server and closes the database connection.
func (n *EventNode) SafeShutdown(ctx context.Context) error {
	// Shutdown the server
	if err := n.Server.Shutdown(ctx); err != nil {
		return err
	}

	// Close the database connection
	if err := n.ShutdownDB(); err != nil {
		return err
	}
	return nil
}

// Code for Auth node
func main() {
	// Create a new HTTP server. This server will be responsible for sending
	// notifications
	server := &EventNode{Node: node.NewNode(), service_ID: utils.NewServiceID("EVNT")}

	fmt.Println("Event Node created...")
	fmt.Println("Initializing database...")

	db, err := sql.Open("sqlite", "./Event_DB.db")
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
		fmt.Println("Starting Event Node...")
		if err := server.Run(fmt.Sprintf(":%s", os.Getenv("EVENT_QUEUE_PORT")), server.RegisterRoutes); !errors.Is(err, http.ErrServerClosed) {
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

	// Shutdown the server. This will block until the server is shutdown.
	if err := server.SafeShutdown(shutdownCtx); err != nil {
		log.Fatalf("\n Event Node shutdown failed: %v", err)
	}
	log.Println("\n Event Node shutdown safely completed")
}

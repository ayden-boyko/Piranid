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

	utils "github.com/ayden-boyko/Piranid/internal"
	node "github.com/ayden-boyko/Piranid/internal/node"
	_ "modernc.org/sqlite"
)

type AuthNode struct {
	*node.Node
	service_ID string
}

func (n *AuthNode) GetServiceID() string { return n.service_ID }

func (n *AuthNode) RegisterRoutes() {
	// TODO Actual route registration for logging server
	n.Node.Router.HandleFunc("/auth_test", func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("Auth recieved...")
		fmt.Fprint(w, "recieved")
	})

}

func (l *AuthNode) ShutdownDB() error {
	db := l.Node.GetDB()
	if sqliteDB, ok := db.(*sql.DB); ok {
		sqliteDB.Close()
		fmt.Println("Database closed...")
		return nil
	}
	return errors.New("database is not *sql.DB, is type " + fmt.Sprintf("%T", db))
}

// SafeShutdown is a function that gracefully stops the server and closes the database connection.
func (n *AuthNode) SafeShutdown(ctx context.Context) error {
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
	server := &AuthNode{Node: node.NewNode(), service_ID: utils.NewServiceID("AUTH")}

	fmt.Println("Auth Node created...")
	fmt.Println("Initializing database...")

	db, err := sql.Open("sqlite", "./Auth_DB.db")
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

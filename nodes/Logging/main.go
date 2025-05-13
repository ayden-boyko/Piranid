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
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
)

// logging variation of node, with influxDB writer
type LoggingNode struct {
	*node.Node // embedding Node
	writeAPI   api.WriteAPI
}

func (l *LoggingNode) GetWriter() api.WriteAPI {
	return l.writeAPI
}

func (l *LoggingNode) SetWriter(writeAPI api.WriteAPI) {
	l.writeAPI = writeAPI
}

// Redefine RegisterRoutes for LoggingNode
func (l *LoggingNode) RegisterRoutes() {
	// TODO Actual route registration for logging server
}

func (l *LoggingNode) ShutdownDB() error {
	db := l.Node.GetDB()
	if influxDB, ok := db.(influxdb2.Client); ok {
		influxDB.Close()
	}
	return errors.New("database is not influxdb2.Client")
}

// SafeShutdown is a function that gracefully stops the server and closes the database connection.
func (l *LoggingNode) SafeShutdown(ctx context.Context) error {
	// Shutdown the server
	if err := l.Server.Shutdown(ctx); err != nil {
		return err
	}

	// Close the influxDB and its writeAPI
	db := l.Node.GetDB()
	if _, ok := db.(influxdb2.Client); ok {
		if err := l.Node.ShutdownDB(); err != nil {
			return err
		}
	}

	// Flush the writeAPI
	l.GetWriter().Flush()

	return nil
}

// Code for Auth node
func main() {
	// Create a new HTTP server. This server will be responsible for running the
	// API and handling requests.
	server := &LoggingNode{Node: node.NewNode()}

	// try to set the writer
	db := server.GetDB()
	if influxClient, ok := db.(influxdb2.Client); ok {
		server.SetWriter(influxClient.WriteAPI(os.Getenv("DB_ORG"), os.Getenv("DB_BUCKET")))
	} else {
		// handle error: db is not an influxdb2.Client
		log.Fatal("db is not an influxdb2.Client \n Change the db to influxdb2.Client")
	}

	// Redis DB created
	server.SetDB(influxdb2.NewClient(fmt.Sprintf("http://localhost:%s", os.Getenv("DB_PORT")), os.Getenv("DB_TOKEN")))

	// Run the server in a sepasrate goroutine. This allows the server to run
	// concurrently with the other code.
	go func() {
		// Run the server and check for errors. This will block until the server
		// is shutdown.
		if err := server.Run(fmt.Sprintf(":%s", os.Getenv("LOGGING_PORT"))); !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Error running Logging Node: %v", err)
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
		log.Fatalf("\n Logging Node shutdown failed: %v", err)
	}
	log.Println("\n Logging Node shutdown safely completed")
}

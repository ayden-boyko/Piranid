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
	"github.com/go-redis/redis"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
)

// logging variation of node, with influxDB writer and redis buffer
type LoggingNode struct {
	*node.Node // embedding Node
	writeAPI   api.WriteAPI
	buffer     *redis.Client
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
	l.Node.Router.HandleFunc("/logging_test", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "hello")
	})
}

func (l *LoggingNode) ShutdownDB() error {
	db := l.Node.GetDB()
	if influxDB, ok := db.(influxdb2.Client); ok {
		influxDB.Close()
	}
	return errors.New("database is not influxdb2.Client")
}

func (l *LoggingNode) SafeShutdown(ctx context.Context) error {
	if err := l.Server.Shutdown(ctx); err != nil {
		return err
	}

	db := l.Node.GetDB()
	if _, ok := db.(influxdb2.Client); ok {
		if err := l.Node.ShutdownDB(); err != nil {
			return err
		}
	}

	l.GetWriter().Flush()

	return nil
}

func main() {
	// Create a new HTTP server. This server will be responsible for running the
	// API and handling requests.
	fmt.Println("Creating a new Logging Node...")
	server := &LoggingNode{Node: node.NewNode(), buffer: redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("localhost:%s", os.Getenv("REDIS_PORT")),
		Password: "", // no password set
		DB:       0,  // use default DB
	})}

	server.SetDB(influxdb2.NewClient(fmt.Sprintf("http://localhost:%s", os.Getenv("DB_PORT")), os.Getenv("DB_TOLKEN")))
	fmt.Println("Database created...")

	// try to set the writer
	db := server.GetDB()
	if influxClient, ok := db.(influxdb2.Client); ok {
		server.SetWriter(influxClient.WriteAPI(os.Getenv("DB_ORG"), os.Getenv("DB_BUCKET")))
	} else {
		fmt.Printf("The type of db is %T\n", db)
		log.Fatal("db is not an influxdb2.Client. Change the db to influxdb2.Client")
	}
	fmt.Println("Writer created...")

	// Run the server in a separate goroutine. This allows the server to run
	// concurrently with the other code.
	go func() {
		fmt.Println("Starting Logging Node...")
		// Run the server and check for errors. This will block until the server is shutdown.
		if err := server.Run(fmt.Sprintf(":%s", os.Getenv("LOGGING_PORT")), server.RegisterRoutes); !errors.Is(err, http.ErrServerClosed) {
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

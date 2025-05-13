package node

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/redis/go-redis/v9"
)

type Node struct {
	Server *http.Server
	Router *http.ServeMux
	db     interface{}
	cache  *redis.Client
}

// NewHTTPServer creates a new HTTPServer with an empty request multiplexer.
//
// The HTTPServer uses a NewServeMux to handle requests, and a cache with a
// default expiration and purge time of 10 minutes.
func NewNode() *Node {
	return &Node{
		Server: &http.Server{},
		Router: http.NewServeMux(),
		db:     nil,
		cache: redis.NewClient(&redis.Options{
			Addr:     fmt.Sprintf("localhost:%s", os.Getenv("REDIS_PORT")),
			Password: "", // No password set
			DB:       0,  // Use default DB
			Protocol: 2,  // Connection protocol
		}),
	}
}

func (n *Node) RegisterRoutes() {}

func (n *Node) ShutdownDB() error { return nil }

func (s *Node) SetDB(newDB interface{}) {
	s.db = newDB
}

func (s *Node) GetDB() interface{} { return s.db }

// Run runs the HTTPServer on the given port.
//
// It first initializes the database connection and executes the SQL script
// from the initfile. If the database connection is already open, it will be
// closed and reopened. If the initfile is empty, the database will not be
// initialized.
//
// Then it registers the routes with the HTTPServer and runs it on the given
// port.
func (s *Node) Run(port string, registerRoutes func()) error {
	// Print the port number
	fmt.Println("HTTPServer running on port " + port)

	if !strings.HasPrefix(port, ":") {
		port = ":" + port
	}

	s.Server.Addr = port // Set the port

	// Register the routes
	registerRoutes()

	// Set the handler to the registered routes
	if s.Router == nil {
		fmt.Println("Router is nil")
		s.Router = http.NewServeMux()
	}
	s.Server.Handler = s.Router

	// Run the server
	return s.Server.ListenAndServe()
}

// SafeShutdown is a function that gracefully stops the server and closes the database connection.
func (s *Node) SafeShutdown(ctx context.Context) error {
	// Shutdown the server
	if err := s.Server.Shutdown(ctx); err != nil {
		return err
	}

	// Close the database connection
	if err := s.ShutdownDB(); err != nil {
		return err
	}

	return nil
}

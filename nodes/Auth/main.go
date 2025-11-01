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
	data_manager "Piranid/pkg/DataManager"

	model "github.com/ayden-boyko/Piranid/nodes/Auth/models"

	"github.com/go-redis/redis"
	_ "modernc.org/sqlite"
)

type AuthNode struct {
	*node.Node
	service_ID string
	cache      *redis.Client
}

func (n *AuthNode) GetServiceID() string { return n.service_ID }

// TODO 2FA

// todo, full auth lifecycle, maybe add new methods to data_manager
// https://claude.ai/chat/88eaf1e7-09f4-49d3-9132-c7464655d584

func (n *AuthNode) RegisterRoutes() {
	db, ok := n.Node.GetDB().(*sql.DB)
	if !ok {
		log.Printf("Error, expected n.Node.GetDB() to be of type *sql.DB, but got %T", n.Node.GetDB())
		return
	}
	credentials_manager, err := data_manager.NewDataManager[model.AuthEntry](db, "credentials")
	if err != nil {
		log.Printf("Error creating manager: %v", err)
	}
	log.Printf("Manager created, %v", credentials_manager)

	auth_code_manager, err := data_manager.NewDataManager[model.AuthCodeEntry](db, "auth_codes")
	if err != nil {
		log.Printf("Error creating manager: %v", err)
	}
	log.Printf("Manager created, %v", auth_code_manager)

	n.Node.Router.HandleFunc("/auth_test", AuthTestHandler)
	n.Node.Router.HandleFunc("/auth", func(w http.ResponseWriter, r *http.Request) {
		if err := AuthHandler(w, r); err != nil {
			log.Printf("Error in Auth handler: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	})
	n.Node.Router.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if err := LoginHandler(w, r, credentials_manager, auth_code_manager); err != nil {
			log.Printf("Error in Login handler: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	})
	n.Node.Router.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := TokenHandler(w, r); err != nil {
			log.Printf("Error in Token handler: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	})

	n.Node.Router.HandleFunc("/Sign_up", func(w http.ResponseWriter, r *http.Request) {
		if err := SignUpHandler(w, r, credentials_manager); err != nil {
			log.Print("Error in SignUp handler: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	})

	n.Node.Router.HandleFunc("/logout", LogoutHandler)
	n.Node.Router.HandleFunc("/userinfo", UserInfoHandler)
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

	// create sqlite DB
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

	// Create a new Redis client
	redisClient := redis.NewClient(&redis.Options{
		Addr:     os.Getenv("REDIS_HOST") + ":" + os.Getenv("REDIS_PORT"),
		Password: "", // no password set
		DB:       0,  // use default DB
	})
	server.cache = redisClient

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

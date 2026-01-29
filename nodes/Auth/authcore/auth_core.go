package core

import (
	"Piranid/node"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/go-redis/redis"

	data_manager "Piranid/pkg/DataManager"

	handler "github.com/ayden-boyko/Piranid/nodes/Auth/handlers"

	model "github.com/ayden-boyko/Piranid/nodes/Auth/models"
)

type AuthNode struct {
	*node.Node
	Service_ID string
	Cache      *redis.Client
}

func (n *AuthNode) GetServiceID() string { return n.Service_ID }

// TODO Caching

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

	n.Node.Router.HandleFunc("/auth_test", handler.AuthTestHandler)
	n.Node.Router.HandleFunc("/auth", func(w http.ResponseWriter, r *http.Request) {
		if err := handler.AuthHandler(w, r); err != nil {
			log.Printf("Error in Auth handler: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	})
	n.Node.Router.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if err := handler.LoginHandler(w, r, credentials_manager, auth_code_manager); err != nil {
			log.Printf("Error in Login handler: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	})
	n.Node.Router.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := handler.TokenHandler(w, r, credentials_manager, auth_code_manager); err != nil {
			log.Printf("Error in Token handler: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	})

	n.Node.Router.HandleFunc("/Sign_up", func(w http.ResponseWriter, r *http.Request) {
		if err := handler.SignUpHandler(w, r, credentials_manager); err != nil {
			log.Printf("Error in SignUp handler: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	})

	n.Node.Router.HandleFunc("/logout", handler.LogoutHandler)
	n.Node.Router.HandleFunc("/userinfo", handler.UserInfoHandler)
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

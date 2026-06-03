package core

import (
	"Piranid/node"
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"net/http"
	"os"

	data_manager "Piranid/pkg/DataManager"

	handler "github.com/ayden-boyko/Piranid/nodes/Auth/handlers"

	model "github.com/ayden-boyko/Piranid/nodes/Auth/models"

	"go.uber.org/zap"
)

type AuthNode struct {
	*node.Node
	Service_ID string
}

func (n *AuthNode) GetServiceID() string { return n.Service_ID }

// TODO Caching
func (n *AuthNode) RegisterRoutes(template embed.FS, ctx context.Context, logger *zap.Logger) {
	db, ok := n.Node.GetDB().(*sql.DB)
	if !ok {
		logger.Error("Error, expected n.Node.GetDB() to be of type *sql.DB, but got", zap.Any("got", n.Node.GetDB()))
		return
	}
	credentials_manager, err := data_manager.NewDataManager[model.AuthEntry](db, "credentials")
	if err != nil {
		logger.Error("Error creating manager", zap.Error(err))
	}
	logger.Info("Manager created", zap.Any("manager", credentials_manager))

	auth_code_manager, err := data_manager.NewDataManager[model.AuthCodeEntry](db, "auth_codes")
	if err != nil {
		logger.Error("Error creating manager", zap.Error(err))
	}
	logger.Info("Manager created", zap.Any("manager", auth_code_manager))

	api_ver := os.Getenv("API_VERSION")

	n.Node.Router.HandleFunc(fmt.Sprintf("/api/%s/auth_test", api_ver), handler.AuthTestHandler)
	n.Node.Router.HandleFunc(fmt.Sprintf("/api/%s/signin", api_ver), func(w http.ResponseWriter, r *http.Request) {
		if err := handler.AuthPageHandler(w, r, template, ctx, logger); err != nil {
			logger.Error("Error in Auth handler", zap.Error(err))
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	})
	n.Node.Router.HandleFunc(fmt.Sprintf("/api/%s/login", api_ver), func(w http.ResponseWriter, r *http.Request) {
		if err := handler.LoginHandler(w, r, credentials_manager, auth_code_manager, ctx, logger); err != nil {
			logger.Error("Error in Login handler", zap.Error(err))
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	})
	n.Node.Router.HandleFunc(fmt.Sprintf("/api/%s/token", api_ver), func(w http.ResponseWriter, r *http.Request) {
		if err := handler.TokenHandler(w, r, credentials_manager, auth_code_manager, ctx, logger); err != nil {
			logger.Error("Error in Token handler", zap.Error(err))
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	})

	n.Node.Router.HandleFunc(fmt.Sprintf("/api/%s/signup_page", api_ver), func(w http.ResponseWriter, r *http.Request) {
		if err := handler.SignUpPageHandler(w, r, template, ctx, logger); err != nil {
			logger.Error("Error in SignUp handler", zap.Error(err))
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	})

	n.Node.Router.HandleFunc(fmt.Sprintf("/api/%s/signup", api_ver), func(w http.ResponseWriter, r *http.Request) {
		if err := handler.SignUpHandler(w, r, credentials_manager, ctx, logger); err != nil {
			logger.Error("Error in SignUp handler", zap.Error(err))
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	})

	n.Node.Router.HandleFunc(fmt.Sprintf("/api/%s/signout", api_ver), handler.LogoutHandler)
	n.Node.Router.HandleFunc(fmt.Sprintf("/api/%s/user_info", api_ver), handler.UserInfoHandler)
}

func (l *AuthNode) ShutdownDB(logger *zap.Logger) error {
	db := l.Node.GetDB()
	if sqliteDB, ok := db.(*sql.DB); ok {
		sqliteDB.Close()
		logger.Info("Database closed...")
		return nil
	}
	return errors.New("database is not *sql.DB, is type " + fmt.Sprintf("%T", db))
}

// SafeShutdown is a function that gracefully stops the server and closes the database connection.
func (n *AuthNode) SafeShutdown(ctx context.Context, logger *zap.Logger) error {
	// Shutdown the server
	if err := n.Server.Shutdown(ctx); err != nil {
		logger.Error("Error shutting down server", zap.Error(err))
		return err
	}

	// Close the database connection
	if err := n.ShutdownDB(logger); err != nil {
		logger.Error("Error shutting down database", zap.Error(err))
		return err
	}
	return nil
}

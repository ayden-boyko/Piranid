package core

import (
	"Piranid/node"
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"net/http"

	data_manager "Piranid/pkg/DataManager"
	"Piranid/pkg/authn"

	handler "github.com/ayden-boyko/Piranid/nodes/Auth/handlers"
	model "github.com/ayden-boyko/Piranid/nodes/Auth/models"

	"go.uber.org/zap"
)

// AuthNode is the OAuth 2.0 authorization server.
type AuthNode struct {
	*node.Node
	Service_ID string
}

// GetServiceID returns the node's per-boot service identifier.
//
// Note this is a fresh UUID on every boot (see Piranid/pkg NewServiceID). It is
// a telemetry label, not an identity: it must never be used as the token
// issuer, because doing so would invalidate every outstanding token each time
// the pod restarted. The issuer is configured separately via AUTH_ISSUER.
func (n *AuthNode) GetServiceID() string { return n.Service_ID }

// RegisterRoutes wires the authorization server endpoints.
//
// These are OAuth-spec paths at the server root rather than under /api/v1.
// The previous layout registered everything behind an API_VERSION prefix with
// no default, so an unset variable produced the pattern "/api//login".
//
// The two OAuth spec endpoints that clients find by convention are served at
// the well-known paths:
//   - GET  /.well-known/jwks.json   public signing keys
//   - GET  /authorize                start the authorization-code flow
//
// Client implementations expect these at fixed locations (RFC 8414 section 3),
// so there is no meaningful version prefix to apply.
//
// Methods are declared on every pattern. Go 1.22's ServeMux routes on method
// and returns 405 for a mismatch; the old methodless patterns accepted any
// verb, including a GET on the token endpoint.
func (n *AuthNode) RegisterRoutes(templatesFS embed.FS, ctx context.Context, logger *zap.Logger) {
	db, ok := n.Node.GetDB().(*sql.DB)
	if !ok {
		logger.Error("expected GetDB() to return *sql.DB", zap.Any("got", n.Node.GetDB()))
		return
	}

	credentialsManager, err := data_manager.NewDataManager[model.AuthEntry](db, "credentials")
	if err != nil {
		// The handlers dereference these managers, so continuing here would
		// turn a startup misconfiguration into a nil-pointer panic on the
		// first request.
		logger.Fatal("could not create credentials manager", zap.Error(err))
		return
	}

	codeManager, err := data_manager.NewDataManager[model.AuthCodeEntry](db, "auth_codes")
	if err != nil {
		logger.Fatal("could not create auth_codes manager", zap.Error(err))
		return
	}

	config := authn.LoadConfig()
	if err := config.Validate(); err != nil {
		// A server that issues tokens without an issuer or audience would
		// produce tokens no service can verify. Fail loudly at startup rather
		// than serving unusable tokens.
		logger.Fatal("invalid auth configuration", zap.Error(err))
		return
	}

	pair, err := authn.LoadKeyPair(config.PrivateKeyPath, config.PublicKeyPath)
	if err != nil {
		logger.Fatal("could not load signing key", zap.Error(err))
		return
	}

	signer, err := authn.NewSigner(pair, config.AccessTokenTTL)
	if err != nil {
		logger.Fatal("could not create token signer", zap.Error(err))
		return
	}

	logger.Info("token signing key loaded",
		zap.String("kid", signer.KeyID()),
		zap.String("issuer", config.Issuer),
		zap.String("audience", config.Audience),
		zap.Duration("access_token_ttl", config.AccessTokenTTL),
		zap.Duration("auth_code_ttl", config.AuthCodeTTL))

	deps := handler.Deps{
		Credentials: credentialsManager,
		Codes:       codeManager,
		Signer:      signer,
		Config:      config,
		Logger:      logger,
	}

	templates, err := loadTemplates(templatesFS, logger)
	if err != nil {
		logger.Fatal("could not parse templates", zap.Error(err))
		return
	}

	// Discovery: public keys. No authentication, and no credential of any
	// kind may appear in the response.
	n.Node.Router.HandleFunc("GET /.well-known/jwks.json", func(w http.ResponseWriter, r *http.Request) {
		handler.JWKSHandler(w, r, deps)
	})

	// Authorization endpoint. Renders the consent form.
	n.Node.Router.HandleFunc("GET /authorize", func(w http.ResponseWriter, r *http.Request) {
		handler.AuthorizeHandler(w, r, templates.Consent, deps)
	})

	// Consent submission. Authenticates the user and issues a code, then
	// redirects back to the client.
	n.Node.Router.HandleFunc("POST /authorize/consent", func(w http.ResponseWriter, r *http.Request) {
		handler.ConsentHandler(w, r, deps)
	})

	// Token endpoint.
	n.Node.Router.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		handler.TokenHandler(w, r, deps)
	})

	// Client and user registration.
	n.Node.Router.HandleFunc("POST /register", func(w http.ResponseWriter, r *http.Request) {
		handler.RegisterHandler(w, r, deps)
	})

	// Health probe for Kubernetes. Deliberately unauthenticated and free of any
	// dependency detail, so it reports liveness without leaking config.
	n.Node.Router.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Logout is retained as an explicit no-op: access tokens are stateless and
	// self-expiring, so there is no server-side session to destroy. Revocation
	// would require either a deny-list (which reintroduces shared state) or a
	// refresh-token store. Left until that design exists, rather than pretending
	// to revoke something it cannot.
	n.Node.Router.HandleFunc("POST /logout", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"no-op","detail":"access tokens are stateless; they expire on their own"}`))
	})
}

// templates holds the parsed HTML templates.
type pageTemplates struct {
	Consent *template.Template
}

// loadTemplates parses the embedded HTML.
//
// Every template in the set is parsed as a set, so a reference from one page to
// a shared layout is checked at startup rather than on the first request that
// happens to render it.
func loadTemplates(templatesFS embed.FS, logger *zap.Logger) (*pageTemplates, error) {
	consent, err := template.ParseFS(templatesFS, "templates/ConsentPage.html")
	if err != nil {
		return nil, fmt.Errorf("parsing ConsentPage.html: %w", err)
	}
	names := make([]string, 0, len(consent.Templates()))
	for _, t := range consent.Templates() {
		names = append(names, t.Name())
	}
	logger.Info("templates loaded", zap.Strings("names", names))
	return &pageTemplates{Consent: consent}, nil
}

func (l *AuthNode) ShutdownDB(logger *zap.Logger) error {
	db := l.Node.GetDB()
	if sqliteDB, ok := db.(*sql.DB); ok {
		if err := sqliteDB.Close(); err != nil {
			logger.Error("error closing database", zap.Error(err))
			return err
		}
		logger.Info("database closed")
		return nil
	}
	return errors.New("database is not *sql.DB, is type " + fmt.Sprintf("%T", db))
}

// SafeShutdown gracefully stops the server and closes the database connection.
func (n *AuthNode) SafeShutdown(ctx context.Context, logger *zap.Logger) error {
	if err := n.Server.Shutdown(ctx); err != nil {
		logger.Error("error shutting down server", zap.Error(err))
		return err
	}
	if err := n.ShutdownDB(logger); err != nil {
		logger.Error("error shutting down database", zap.Error(err))
		return err
	}
	return nil
}

package main

import (
	"context"
	"embed"
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
	"Piranid/pkg/authn"
	"Piranid/pkg/dbutil"
	telemetry "Piranid/pkg/telemetry"

	core "github.com/ayden-boyko/Piranid/nodes/Auth/authcore"

	"github.com/redis/go-redis/v9"
)

// TODO: terminate TLS. The node currently serves plain HTTP, so client
// credentials and authorization codes travel in cleartext. In-cluster this is
// acceptable only behind a mesh or an ingress that terminates TLS.

//go:embed templates/*
var TemplatesFS embed.FS

// main boots the OAuth 2.0 authorization server.
func main() {
	ctx := context.Background()

	fmt.Println("Creating a new Auth Node...")

	server := &core.AuthNode{Node: node.NewNode(), Service_ID: utils.NewServiceID("AUTH")}

	fmt.Println("Auth Node created...")

	// Validate token configuration before opening the database, so a missing
	// issuer is reported as a configuration error rather than surfacing later
	// as an unverifiable token.
	config := authn.LoadConfig()
	if err := config.Validate(); err != nil {
		log.Fatalf("auth configuration invalid: %v", err)
	}

	// SQLite holds clients, credentials, and unconsumed authorization codes.
	// The DSN is now the real file path; it was previously a Postgres-shaped
	// string passed to the SQLite driver.
	dsn := os.Getenv("AUTH_DB_PATH")
	if dsn == "" {
		dsn = "auth.db"
	}
	schema := os.Getenv("AUTH_SCHEMA_PATH")
	if schema == "" {
		schema = "database/Schema.sql"
	}

	// Open with WAL journaling and a busy timeout. Without the timeout,
	// simultaneous token requests collide on SQLite's single write lock and
	// fail outright with SQLITE_BUSY rather than queueing.
	db, err := dbutil.OpenSQLite(dsn)
	if err != nil {
		log.Fatalf("Error opening database: %v", err)
	}
	server.Node.SetDB(db)

	script, err := os.ReadFile(schema)
	if err != nil {
		log.Fatalf("Error reading schema %q: %v", schema, err)
	}
	if err := dbutil.ApplySchema(db, string(script)); err != nil {
		log.Fatalf("Error applying schema: %v", err)
	}

	// Redis is available for caching. Not currently required for correctness:
	// authorization codes are read from SQLite on the token request path.
	redisHost := os.Getenv("REDIS_HOST")
	redisPort := os.Getenv("REDIS_PORT")
	if redisHost != "" && redisPort != "" {
		redisClient := redis.NewClient(&redis.Options{
			Addr:     redisHost + ":" + redisPort,
			Password: os.Getenv("REDIS_PASSWORD"),
			DB:       0,
		})
		server.Node.SetCache(redisClient)
	}

	// Telemetry.
	collectorAddr := os.Getenv("OTEL_COLLECTOR_ADDR")
	if collectorAddr == "" {
		collectorAddr = "localhost:4317"
	}
	otelShutdown, err := telemetry.SetupOTelSDK(ctx, "Auth Node", collectorAddr)
	if err != nil {
		log.Fatalf("failed to set up telemetry: %v", err)
	}
	defer otelShutdown(ctx)

	// Logging. This previously named the service "notifications".
	logger, err := telemetry.NewLogger("auth")
	if err != nil {
		log.Fatalf("failed to setup logger: %v", err)
	}
	defer logger.Sync()

	// The port variable was AUTH_PORT; the Kubernetes manifest set
	// AUTH_SERVICE_PORT, so the server bound to ":".
	port := os.Getenv("AUTH_PORT")
	if port == "" {
		port = "8081"
	}

	go func() {
		fmt.Println("Starting Auth Node...")
		if err := server.Run(":"+port, func() {
			server.RegisterRoutes(TemplatesFS, ctx, logger)
		}); !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Error running Auth Node: %v", err)
		}
	}()

	// Wait for a termination signal, then drain.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 10*time.Second)
	defer shutdownCancel()

	if err := server.SafeShutdown(shutdownCtx, logger); err != nil {
		log.Fatalf("\n Auth Node shutdown failed: %v", err)
	}
	log.Println("\n Auth Node shutdown safely completed")
}

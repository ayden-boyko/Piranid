package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	utils "Piranid/pkg"
	"Piranid/pkg/dbutil"
	"Piranid/pkg/telemetry"

	v1 "Piranid/pkg/proto/notifications/v1"

	handlers "github.com/ayden-boyko/Piranid/nodes/Notifications/handlers"
	core "github.com/ayden-boyko/Piranid/nodes/Notifications/notifcore"

	"github.com/trycourier/courier-go/v2"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// DefaultNotificationPort is used when NOTIFICATION_PORT is unset.
//
// The Kubernetes manifest previously set NOTIFICATION_SERVICE_PORT, a name the
// code never read, so the node listened on 8084 by accident.
const DefaultNotificationPort = "8084"

func main() {
	ctx := context.Background()

	collectorAddr := os.Getenv("OTEL_COLLECTOR_ADDR")
	if collectorAddr == "" {
		collectorAddr = "localhost:4317"
	}
	otelShutdown, err := telemetry.SetupOTelSDK(ctx, "notifications", collectorAddr)
	if err != nil {
		log.Fatalf("failed to set up telemetry: %v", err)
	}
	defer otelShutdown(ctx)

	logger, err := telemetry.NewLogger("notifications")
	if err != nil {
		log.Fatalf("failed to setup logger: %v", err)
	}
	defer logger.Sync()

	port := os.Getenv("NOTIFICATION_PORT")
	if port == "" {
		logger.Warn("NOTIFICATION_PORT is not set, using default",
			zap.String("port", DefaultNotificationPort))
		port = DefaultNotificationPort
	}

	// Courier. courier.CreateClient does not validate its token, so a missing
	// token only surfaces at the first send. Fail fast instead.
	courierToken := os.Getenv("COURIER_TOKEN")
	if courierToken == "" {
		logger.Error("COURIER_TOKEN is not set; notification delivery cannot work")
		os.Exit(1)
	}
	messager := courier.CreateClient(courierToken, nil)

	server := &core.NotificationNode{
		Messager:   messager,
		Service_ID: utils.NewServiceID("NOTF"),
		Logger:     logger,
	}

	// Storage. Paths are configurable; the previous ones were relative to the
	// working directory, so running outside the container silently created a new
	// empty database and then failed on the missing schema.
	db, err := openDatabase(logger)
	if err != nil {
		logger.Error("could not open database", zap.Error(err))
		os.Exit(1)
	}
	if err := server.AttachDB(db); err != nil {
		logger.Error("could not attach notifications store", zap.Error(err))
		os.Exit(1)
	}

	// gRPC server, with tracing and bearer verification.
	//
	// This was grpc.NewServer() with no options at all: no interceptors, no TLS,
	// and no recovery.
	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			telemetry.UnaryServerInterceptor("notifications"),
			buildAuthInterceptor(logger),
		),
		grpc.ChainStreamInterceptor(
			telemetry.StreamServerInterceptor("notifications"),
			buildStreamAuthInterceptor(logger),
		),
	)

	notifHandler := handlers.NewNotificationHandler(server, logger)
	v1.RegisterNotifierServer(grpcServer, notifHandler)

	// Reflection is useful for debugging but exposes the full service
	// descriptor. Enable it only when asked.
	if os.Getenv("GRPC_REFLECTION") == "true" {
		reflection.Register(grpcServer)
		logger.Info("gRPC reflection enabled")
	}

	listener, err := net.Listen("tcp", ":"+port)
	if err != nil {
		logger.Error("could not listen", zap.String("port", port), zap.Error(err))
		os.Exit(1)
	}

	// MQ consumer.
	mqCfg := core.LoadMQConfig()
	mqCtx, stopMQ := context.WithCancel(ctx)
	defer stopMQ()

	mqErr := make(chan error, 1)
	go func() { mqErr <- server.StartMQListener(mqCtx, mqCfg, logger) }()

	go func() {
		logger.Info("gRPC server listening", zap.String("port", port))
		if err := grpcServer.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			logger.Error("gRPC server stopped", zap.Error(err))
			os.Exit(1)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigChan:
		logger.Info("shutdown signal received", zap.String("signal", sig.String()))
	case err := <-mqErr:
		if err != nil {
			logger.Error("message queue consumer failed", zap.Error(err))
		}
	}

	// Stop accepting RPCs, drain in-flight ones, then release resources.
	//
	// grpcServer.GracefulStop was never called anywhere before, so in-flight
	// RPCs were cut off at the shutdown deadline.
	stopped := make(chan struct{})
	go func() {
		grpcServer.GracefulStop()
		close(stopped)
	}()

	select {
	case <-stopped:
		logger.Info("gRPC server drained")
	case <-time.After(10 * time.Second):
		logger.Warn("gRPC drain timed out, forcing stop")
		grpcServer.Stop()
	}

	stopMQ()
	listener.Close()

	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := server.SafeShutdown(shutdownCtx); err != nil {
		logger.Error("shutdown failed", zap.Error(err))
	}

	log.Println("\n Notification Node shutdown safely completed")
}

// openDatabase opens the notifications store with the pragmas that make
// concurrent writes safe.
//
// It uses dbutil.OpenSQLite rather than the legacy helper, which passed a
// Postgres-shaped DSN to the SQLite driver and executed the schema on a
// connection it then discarded.
func openDatabase(logger *zap.Logger) (*sql.DB, error) {
	path := os.Getenv("NOTIFICATION_DB_PATH")
	if path == "" {
		path = "./notification.db"
	}

	db, err := dbutil.OpenSQLite(path)
	if err != nil {
		return nil, err
	}

	schemaPath := os.Getenv("NOTIFICATION_SCHEMA_PATH")
	if schemaPath == "" {
		schemaPath = "database/Schema.sql"
	}
	script, err := os.ReadFile(schemaPath)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("reading schema %q: %w", schemaPath, err)
	}
	if err := dbutil.ApplySchema(db, string(script)); err != nil {
		db.Close()
		return nil, err
	}

	logger.Info("notifications store ready", zap.String("path", path))
	return db, nil
}

// buildAuthInterceptor constructs bearer verification from the environment.
//
// AUTH_JWKS_URL, AUTH_ISSUER and AUTH_AUDIENCE must match the auth node's.
func buildAuthInterceptor(logger *zap.Logger) grpc.UnaryServerInterceptor {
	return handlers.UnaryAuthInterceptor(handlers.AuthInterceptorOptions{
		JWKSURL:        os.Getenv("AUTH_JWKS_URL"),
		Issuer:         os.Getenv("AUTH_ISSUER"),
		Audience:       os.Getenv("AUTH_AUDIENCE"),
		AllowAnonymous: os.Getenv("AUTH_ALLOW_ANONYMOUS") == "true",
	}, zapAdapter{logger})
}

func buildStreamAuthInterceptor(logger *zap.Logger) grpc.StreamServerInterceptor {
	return handlers.StreamAuthInterceptor(handlers.AuthInterceptorOptions{
		JWKSURL:        os.Getenv("AUTH_JWKS_URL"),
		Issuer:         os.Getenv("AUTH_ISSUER"),
		Audience:       os.Getenv("AUTH_AUDIENCE"),
		AllowAnonymous: os.Getenv("AUTH_ALLOW_ANONYMOUS") == "true",
	}, zapAdapter{logger})
}

// zapAdapter bridges zap to the small logging interface handlers require.
type zapAdapter struct{ l *zap.Logger }

func (a zapAdapter) Warn(msg string, fields ...any) {
	if a.l == nil {
		return
	}
	out := make([]zap.Field, 0, len(fields)/2)
	for i := 0; i+1 < len(fields); i += 2 {
		out = append(out, zap.Any(fmt.Sprint(fields[i]), fields[i+1]))
	}
	a.l.Warn(msg, out...)
}

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

	"go.uber.org/zap"

	node "Piranid/node"
	utils "Piranid/pkg"
	telemetry "Piranid/pkg/telemetry"

	core "github.com/ayden-boyko/Piranid/nodes/Event_Queue/eventcore"
)

// DefaultQueuePort is used when RABBIT_MQ_PORT is unset.
const DefaultQueuePort = "5672"

func main() {
	ctx := context.Background()

	fmt.Println("Creating a new Event Queue Node...")

	// Telemetry first, so a configuration failure is reported through the same
	// pipeline as everything else.
	collectorAddr := os.Getenv("OTEL_COLLECTOR_ADDR")
	if collectorAddr == "" {
		collectorAddr = "localhost:4317"
	}
	otelShutdown, err := telemetry.SetupOTelSDK(ctx, "event_queue", collectorAddr)
	if err != nil {
		log.Fatalf("failed to set up telemetry: %v", err)
	}
	defer otelShutdown(ctx)

	// The service name here used to be "Auth Node" and the logger name
	// "notifications", so dashboards attributed this node's telemetry to two
	// other services.
	logger, err := telemetry.NewLogger("event_queue")
	if err != nil {
		log.Fatalf("failed to setup logger: %v", err)
	}
	defer logger.Sync()

	port := os.Getenv("EVENT_QUEUE_PORT")
	if port == "" {
		logger.Error("EVENT_QUEUE_PORT is not set")
		os.Exit(1)
	}

	brokerHost := os.Getenv("RABBIT_MQ_HOST")
	if brokerHost == "" {
		brokerHost = "rabbitmq"
	}
	brokerPort := os.Getenv("RABBIT_MQ_PORT")
	if brokerPort == "" {
		// Previously this was log.Panic, which meant the variable was
		// mandatory. The Kubernetes manifest never set it, so the pod
		// crash-looped before reaching any handler. A documented default is
		// better than an unstartable deployment.
		logger.Warn("RABBIT_MQ_PORT is not set, using default", zap.String("port", brokerPort))
		brokerPort = DefaultQueuePort
	}
	brokerUser := envOr("RABBIT_MQ_USER", "guest")
	brokerPass := envOr("RABBIT_MQ_PASSWORD", "guest")

	brokerURL := fmt.Sprintf("amqp://%s:%s@%s:%s/", brokerUser, brokerPass, brokerHost, brokerPort)

	// Fail fast on a bad broker configuration, but do not log.Panic: that
	// skips the deferred telemetry shutdown and logger flush.
	server, err := core.NewEventNode(node.NewNode(), utils.NewServiceID("EVNT"), brokerURL, logger)
	if err != nil {
		logger.Error("could not connect to RabbitMQ", zap.Error(err))
		os.Exit(1)
	}

	go func() {
		fmt.Println("Starting Event Queue Node...")
		if err := server.Run(":"+port, func() {
			server.RegisterRoutes(ctx, logger)
		}); !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server stopped", zap.Error(err))
			os.Exit(1)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 10*time.Second)
	defer shutdownCancel()

	if err := server.SafeShutdown(shutdownCtx, logger); err != nil {
		logger.Error("shutdown failed", zap.Error(err))
		os.Exit(1)
	}
	log.Println("\n Event Queue Node shutdown safely completed")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

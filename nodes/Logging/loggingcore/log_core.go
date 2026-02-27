package loggingcore

import (
	"Piranid/node"
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-redis/redis"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
)

// logging variation of node, with influxDB writer and redis buffer
type LoggingNode struct {
	*node.Node // embedding Node
	writeAPI   api.WriteAPI
	Buffer     *redis.Client
	Service_ID string
}

func (n *LoggingNode) GetServiceID() string { return n.Service_ID }

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

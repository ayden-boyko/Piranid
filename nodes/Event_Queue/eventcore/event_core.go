package eventcore

import (
	"Piranid/node"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	handler "github.com/ayden-boyko/Piranid/nodes/Event_Queue/handlers"
	"github.com/ayden-boyko/Piranid/nodes/Event_Queue/models"

	amqp "github.com/rabbitmq/amqp091-go"
)

type EventNode struct {
	*node.Node
	Service_ID string
	Services   models.Services
}

func (n *EventNode) GetServiceID() string { return n.Service_ID }

func (n *EventNode) RegisterRoutes(conn *amqp.Connection) {
	api_ver := os.Getenv("API_VERSION")

	n.Node.Router.HandleFunc(fmt.Sprintf("/api/%s/event_test", api_ver), handler.EventTestHandler)

	n.Node.Router.HandleFunc(fmt.Sprintf("GET /api/%s/services/{uuid}", api_ver), func(w http.ResponseWriter, r *http.Request) {
		response, err := handler.GetServiceHandler(w, r, &n.Services)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	})

	n.Node.Router.HandleFunc(fmt.Sprintf("POST /api/%s/services/{uuid}", api_ver), func(w http.ResponseWriter, r *http.Request) {
		err := handler.AddServiceHandler(w, r, &n.Services, conn)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	})

	n.Node.Router.HandleFunc(fmt.Sprintf("DELETE /api/%s/services/{uuid}", api_ver), func(w http.ResponseWriter, r *http.Request) {
		err := handler.RemoveServiceHandler(w, r, &n.Services, conn)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	})

	n.Node.Router.HandleFunc(fmt.Sprintf("GET /api/%s/services/{uuid}/queue/{uuid}", api_ver), func(w http.ResponseWriter, r *http.Request) {
		response, err := handler.GetQueueHandler(w, r, &n.Services)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	})

	n.Node.Router.HandleFunc(fmt.Sprintf("POST /api/%s/services/{uuid}/queue/{uuid}", api_ver), func(w http.ResponseWriter, r *http.Request) {
		err := handler.AddQueueHandler(w, r, &n.Services)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	})

	n.Node.Router.HandleFunc(fmt.Sprintf("DELETE/api/%s/services/{uuid}/queue/{uuid}", api_ver), func(w http.ResponseWriter, r *http.Request) {
		err := handler.RemoveQueueHandler(w, r, &n.Services, conn)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	})

	n.Node.Router.HandleFunc(fmt.Sprintf("/api/%s/services", api_ver), func(w http.ResponseWriter, r *http.Request) {})

}

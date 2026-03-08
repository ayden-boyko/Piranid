package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ayden-boyko/Piranid/nodes/Event_Queue/models"
	transactions "github.com/ayden-boyko/Piranid/nodes/Event_Queue/transactions"

	amqp "github.com/rabbitmq/amqp091-go"
)

func EventTestHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Auth received...")
	fmt.Fprint(w, "received")
}

func AddServiceHandler(w http.ResponseWriter, r *http.Request, ServiceQueues *models.Services, conn *amqp.Connection) error {
	var req transactions.AddServiceRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		return err
	}
	fmt.Fprint(w, "Adding service...")
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	//check if services queuse already exist for this service uuid
	if _, err := ServiceQueues.GetService(req.ServiceId); err == nil {
		return fmt.Errorf("Service %s already exists", req.ServiceId)
	} else {
		ServiceQueues.AddService(&req, ch)
	}
	return nil
}

func AddQueueHandler(w http.ResponseWriter, r *http.Request, ServiceQueues *models.Services) error {
	var req transactions.AddQueueRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		return err
	}
	fmt.Fprint(w, "Adding Queue...")
	var service *models.Service
	//Check if service exists
	if service, err = ServiceQueues.GetService(req.ServiceId); err != nil {
		return fmt.Errorf("Service %s does not exist", req.ServiceId)
	}
	//check if queue already exists
	if _, err := ServiceQueues.GetServiceQueue(req.ServiceId, req.QueueName); err == nil {
		return fmt.Errorf("Queue %s already exists", req.QueueName)
	}
	ServiceQueues.AddServiceQueue(&req, service)
	fmt.Fprint(w, "Added Queue...")

	return nil
}

func GetQueueHandler(w http.ResponseWriter, r *http.Request, ServiceQueues *models.Services) (transactions.GetQueueResponse, error) {
	var req transactions.GetQueueRequest
	fmt.Fprint(w, "Getting queue...")

	var response transactions.GetQueueResponse
	var queue *models.ServiceQueue

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		return response, err
	}
	//Check if service exists
	if _, err := ServiceQueues.GetService(req.ServiceId); err != nil {
		return response, fmt.Errorf("Service %s does not exist", req.ServiceId)
	}
	//get queue
	if queue, err = ServiceQueues.GetServiceQueue(req.ServiceId, req.QueueName); err != nil {
		return response, fmt.Errorf("Queue %s does not exist", req.QueueName)
	}
	fmt.Fprint(w, "Got queue...")
	response.Name = queue.QueueName
	response.ServiceId = queue.ServiceId
	response.Loggable = queue.Loggable
	response.Tags = queue.Tags
	return response, nil
}

func GetServiceHandler(w http.ResponseWriter, r *http.Request, Services *models.Services) (transactions.GetServiceResponse, error) {
	fmt.Fprint(w, "Getting service...")
	var req transactions.GetServiceRequest
	var response transactions.GetServiceResponse
	var service *models.Service

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		return response, err
	}

	if service, err = Services.GetService(req.ServiceId); err != nil {
		return response, fmt.Errorf("Service %s does not exist", req.ServiceId)
	}

	fmt.Fprint(w, "Got service...")
	response.ServiceId = service.ServiceId

	// Declare with the correct type matching GetServiceResponse.Queues
	queuesMap := make(map[string]transactions.GetQueueResponse)

	for _, queue := range service.Queues {
		// Use = not :=, and repeat the struct type to match the map
		queuesMap[queue.QueueName] = transactions.GetQueueResponse{
			ServiceId: queue.ServiceId,
			Name:      queue.QueueName,
			Tags:      queue.Tags,
			Loggable:  queue.Loggable,
		}
	}

	response.Queues = queuesMap
	return response, nil
}

func GetAllServicesHandler(w http.ResponseWriter, r *http.Request, Services *models.Services) (transactions.GetAllServicesResponse, error) {
	var response transactions.GetAllServicesResponse

	services := Services.GetAllServices()

	for _, service := range services {
		queuesMap := make(map[string]transactions.GetQueueResponse)
		for _, queue := range service.Queues {
			queuesMap[queue.QueueName] = transactions.GetQueueResponse{
				ServiceId: queue.ServiceId,
				Name:      queue.QueueName,
				Tags:      queue.Tags,
				Loggable:  queue.Loggable,
			}
		}
		response.Services = append(response.Services, transactions.GetServiceResponse{
			ServiceId: service.ServiceId,
			Queues:    queuesMap,
		})
	}

	return response, nil
}

/*
Workflow for removing services:
1. Service signals intent to delete (marks itself as "draining")
2. MQ server stops routing new messages to that service's queues
3. Queues finish processing or dead-letter remaining messages
4. MQ server deletes the queues and notifies the service
5. Service shuts down

same thing for queues
*/

func RemoveQueueHandler(w http.ResponseWriter, r *http.Request, Services *models.Services, conn *amqp.Connection) error {
	var req transactions.RemoveQueueRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return fmt.Errorf("failed to decode request: %w", err)
	}

	// 1. Mark queue as draining
	service, err := Services.GetService(req.ServiceId)
	if err != nil {
		return fmt.Errorf("service %s does not exist", req.ServiceId)
	}

	if err := service.SetQueueDraining(req.QueueName); err != nil {
		return fmt.Errorf("failed to mark queue %s as draining: %w", req.QueueName, err)
	}

	// 2. Stop routing new messages to queue
	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("failed to open channel: %w", err)
	}
	defer ch.Close()

	// 3. Wait for queue to finish processing or dead-letter remaining messages
	for {
		queue, err := ch.QueueDeclarePassive(req.QueueName, false, false, false, false, nil)
		if err != nil {
			return fmt.Errorf("failed to inspect queue %s: %w", req.QueueName, err)
		}
		if queue.Messages == 0 {
			break
		}
		fmt.Printf("Queue %s has %d messages remaining, waiting...\n", req.QueueName, queue.Messages)
		time.Sleep(1 * time.Second)
	}

	// 4. Delete the queue and notify the service
	_, err = ch.QueueDelete(req.QueueName, false, false, false)
	if err != nil {
		return fmt.Errorf("failed to delete queue %s: %w", req.QueueName, err)
	}

	if err := service.RemoveQueue(req.QueueName); err != nil {
		return fmt.Errorf("failed to remove queue %s from service: %w", req.QueueName, err)
	}

	fmt.Printf("Queue %s removed successfully\n", req.QueueName)
	w.WriteHeader(http.StatusOK)
	return nil
}

func RemoveServiceHandler(w http.ResponseWriter, r *http.Request, Services *models.Services, conn *amqp.Connection) error {
	var req transactions.RemoveServiceRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return fmt.Errorf("failed to decode request: %w", err)
	}

	// 1. Mark service as draining
	service, err := Services.GetService(req.ServiceId)
	if err != nil {
		return fmt.Errorf("service %s does not exist", req.ServiceId)
	}

	if err := Services.SetServiceDraining(req.ServiceId); err != nil {
		return fmt.Errorf("failed to mark service %s as draining: %w", req.ServiceId, err)
	}

	// 2 & 3. Stop routing and drain each queue
	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("failed to open channel: %w", err)
	}
	defer ch.Close()

	for _, queue := range service.Queues {
		if err := service.SetQueueDraining(queue.QueueName); err != nil {
			return fmt.Errorf("failed to mark queue %s as draining: %w", queue.QueueName, err)
		}
		isDurable := queue.SearchTags("durable")
		for {
			// Only QueueName is needed
			q, err := ch.QueueDeclarePassive(queue.QueueName, isDurable, false, false, false, nil)
			if err != nil {
				return fmt.Errorf("failed to inspect queue %s: %w", queue.QueueName, err)
			}
			if q.Messages == 0 {
				break
			}
			fmt.Printf("Queue %s has %d messages remaining, waiting...\n", queue.QueueName, q.Messages)
			time.Sleep(1 * time.Second)
		}

		// 4. Delete each queue
		if _, err := ch.QueueDelete(queue.QueueName, false, false, false); err != nil {
			return fmt.Errorf("failed to delete queue %s: %w", queue.QueueName, err)
		}
		fmt.Printf("Queue %s deleted\n", queue.QueueName)
	}

	// 4. Remove service and notify
	if err := Services.RemoveService(req.ServiceId); err != nil {
		return fmt.Errorf("failed to remove service %s: %w", req.ServiceId, err)
	}

	// 5. Service can now shut down
	fmt.Printf("Service %s removed successfully\n", req.ServiceId)
	w.WriteHeader(http.StatusOK)
	return nil
}

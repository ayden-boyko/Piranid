package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ayden-boyko/Piranid/nodes/Event_Queue/models"
	transactions "github.com/ayden-boyko/Piranid/nodes/Event_Queue/transactions"
	"go.uber.org/zap"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"

	amqp "github.com/rabbitmq/amqp091-go"
)

var tracer = otel.Tracer("event_queue/handlers")

func EventTestHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Auth received...")
	fmt.Fprint(w, "received")
}

func AddServiceHandler(w http.ResponseWriter, r *http.Request, ServiceQueues *models.Services, conn *amqp.Connection, ctx context.Context, logger *zap.Logger) error {
	ctx, span := tracer.Start(ctx, "AddServiceHandler")
	defer span.End()

	var req transactions.AddServiceRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	logger.Info("Adding service...")
	ch, err := conn.Channel()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	//check if services queuse already exist for this service uuid
	if _, err := ServiceQueues.GetService(req.ServiceId); err == nil {
		span.SetStatus(codes.Error, "Service already exists")
		span.RecordError(fmt.Errorf("Service %s already exists", req.ServiceId))
		msg := fmt.Sprintf("Service %s already exists", req.ServiceId)
		logger.Error(msg)
		return fmt.Errorf("%s", msg)
	} else {
		span.SetStatus(codes.Ok, "")
		ServiceQueues.AddService(&req, ch)
	}
	return nil
}

func AddQueueHandler(w http.ResponseWriter, r *http.Request, ServiceQueues *models.Services, ctx context.Context, logger *zap.Logger) error {
	ctx, span := tracer.Start(ctx, "AddQueueHandler")
	defer span.End()

	var req transactions.AddQueueRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	logger.Info("Adding Queue...")
	var service *models.Service
	//Check if service exists
	if service, err = ServiceQueues.GetService(req.ServiceId); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		msg := fmt.Sprintf("Service %s does not exist", req.ServiceId)
		logger.Error(msg)
		return fmt.Errorf("%s", msg)
	}
	//check if queue already exists
	if _, err := ServiceQueues.GetServiceQueue(req.ServiceId, req.QueueName); err == nil {
		span.SetStatus(codes.Error, "Queue already exists")
		span.RecordError(fmt.Errorf("Queue %s already exists", req.QueueName))
		msg := fmt.Sprintf("Queue %s already exists", req.QueueName)
		logger.Error(msg)
		return fmt.Errorf("%s", msg)
	}
	span.SetStatus(codes.Ok, "")
	ServiceQueues.AddServiceQueue(&req, service)
	logger.Info("Added Queue...")

	return nil
}

func GetQueueHandler(w http.ResponseWriter, r *http.Request, ServiceQueues *models.Services, ctx context.Context, logger *zap.Logger) (transactions.GetQueueResponse, error) {
	ctx, span := tracer.Start(ctx, "GetQueueHandler")
	defer span.End()

	var req transactions.GetQueueRequest
	logger.Info("Getting queue...")

	var response transactions.GetQueueResponse
	var queue *models.ServiceQueue

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return response, err
	}
	//Check if service exists
	if _, err := ServiceQueues.GetService(req.ServiceId); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		msg := fmt.Sprintf("Service %s does not exist", req.ServiceId)
		logger.Error(msg)
		return response, fmt.Errorf("%s", msg)
	}
	//get queue
	if queue, err = ServiceQueues.GetServiceQueue(req.ServiceId, req.QueueName); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		msg := fmt.Sprintf("Queue %s does not exist", req.QueueName)
		logger.Error(msg)
		return response, fmt.Errorf("%s", msg)
	}
	span.SetStatus(codes.Ok, "")
	logger.Info("Got queue...")
	response.Name = queue.QueueName
	response.ServiceId = queue.ServiceId
	response.Loggable = queue.Loggable
	response.Tags = queue.Tags
	return response, nil
}

func GetServiceHandler(w http.ResponseWriter, r *http.Request, Services *models.Services, ctx context.Context, logger *zap.Logger) (transactions.GetServiceResponse, error) {
	ctx, span := tracer.Start(ctx, "GetServiceHandler")
	defer span.End()

	logger.Info("Getting service...")
	var req transactions.GetServiceRequest
	var response transactions.GetServiceResponse
	var service *models.Service

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return response, err
	}

	if service, err = Services.GetService(req.ServiceId); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		msg := fmt.Sprintf("Service %s does not exist", req.ServiceId)
		logger.Error(msg)
		return response, fmt.Errorf("%s", msg)
	}

	logger.Info("Got service...")
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

	logger.Info("Mapped queues for service...")
	span.SetStatus(codes.Ok, "")
	response.Queues = queuesMap
	return response, nil
}

func GetAllServicesHandler(w http.ResponseWriter, r *http.Request, Services *models.Services, ctx context.Context, logger *zap.Logger) (transactions.GetAllServicesResponse, error) {
	ctx, span := tracer.Start(ctx, "GetAllServicesHandler")
	defer span.End()
	logger.Info("Getting all services...")
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

	span.SetStatus(codes.Ok, "")
	logger.Info("Got all services...")
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

func RemoveQueueHandler(w http.ResponseWriter, r *http.Request, Services *models.Services, conn *amqp.Connection, ctx context.Context, logger *zap.Logger) error {
	ctx, span := tracer.Start(ctx, "RemoveQueueHandler")
	defer span.End()

	logger.Info("Removing queue...")

	var req transactions.RemoveQueueRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		msg := fmt.Sprintf("Failed to decode request: %v", err)
		logger.Error(msg)
		return fmt.Errorf("%s", msg)
	}

	// 1. Mark queue as draining
	service, err := Services.GetService(req.ServiceId)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		msg := fmt.Sprintf("Service %s does not exist", req.ServiceId)
		logger.Error(msg)
		return fmt.Errorf("%s", msg)
	}

	if err := service.SetQueueDraining(req.QueueName); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		msg := fmt.Sprintf("Failed to mark queue %s as draining: %v", req.QueueName, err)
		logger.Error(msg)
		return fmt.Errorf("%s", msg)
	}

	// 2. Stop routing new messages to queue
	ch, err := conn.Channel()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		msg := fmt.Sprintf("Failed to open channel: %v", err)
		logger.Error(msg)
		return fmt.Errorf("%s", msg)
	}
	defer ch.Close()

	// 3. Wait for queue to finish processing or dead-letter remaining messages
	for {
		queue, err := ch.QueueDeclarePassive(req.QueueName, false, false, false, false, nil)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			msg := fmt.Sprintf("Failed to inspect queue %s: %v", req.QueueName, err)
			logger.Error(msg)
			return fmt.Errorf("%s", msg)
		}
		if queue.Messages == 0 {
			break
		}
		msg := fmt.Sprintf("Queue %s has %d messages remaining, waiting...", req.QueueName, queue.Messages)
		logger.Info(msg)
		time.Sleep(1 * time.Second)
	}

	// 4. Delete the queue and notify the service
	_, err = ch.QueueDelete(req.QueueName, false, false, false)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		msg := fmt.Sprintf("Failed to delete queue %s: %v", req.QueueName, err)
		logger.Error(msg)
		return fmt.Errorf("%s", msg)
	}

	if err := service.RemoveQueue(req.QueueName); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		msg := fmt.Sprintf("Failed to remove queue %s from service: %v", req.QueueName, err)
		logger.Error(msg)
		return fmt.Errorf("%s", msg)
	}

	span.SetStatus(codes.Ok, "")
	msg := fmt.Sprintf("Queue %s removed successfully", req.QueueName)
	logger.Info(msg)
	w.WriteHeader(http.StatusOK)
	return nil
}

func RemoveServiceHandler(w http.ResponseWriter, r *http.Request, Services *models.Services, conn *amqp.Connection, ctx context.Context, logger *zap.Logger) error {
	ctx, span := tracer.Start(ctx, "RemoveServiceHandler")
	defer span.End()

	var req transactions.RemoveServiceRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		msg := fmt.Sprintf("Failed to decode request: %v", err)
		logger.Error(msg)
		return fmt.Errorf("%s", msg)
	}

	// 1. Mark service as draining
	service, err := Services.GetService(req.ServiceId)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		msg := fmt.Sprintf("Service %s does not exist", req.ServiceId)
		logger.Error(msg)
		return fmt.Errorf("%s", msg)
	}

	if err := Services.SetServiceDraining(req.ServiceId); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		msg := fmt.Sprintf("Failed to mark service %s as draining: %v", req.ServiceId, err)
		logger.Error(msg)
		return fmt.Errorf("%s", msg)
	}

	// 2 & 3. Stop routing and drain each queue
	ch, err := conn.Channel()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		msg := fmt.Sprintf("Failed to open channel: %v", err)
		logger.Error(msg)
		return fmt.Errorf("%s", msg)
	}
	defer ch.Close()

	for _, queue := range service.Queues {
		if err := service.SetQueueDraining(queue.QueueName); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			msg := fmt.Sprintf("Failed to mark queue %s as draining: %v", queue.QueueName, err)
			logger.Error(msg)
			return fmt.Errorf("%s", msg)
		}
		isDurable := queue.SearchTags("durable")
		for {
			// Only QueueName is needed
			q, err := ch.QueueDeclarePassive(queue.QueueName, isDurable, false, false, false, nil)
			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				msg := fmt.Sprintf("Failed to inspect queue %s: %v", queue.QueueName, err)
				logger.Error(msg)
				return fmt.Errorf("%s", msg)
			}
			if q.Messages == 0 {
				break
			}
			msg := fmt.Sprintf("Queue %s has %d messages remaining, waiting...", queue.QueueName, q.Messages)
			logger.Info(msg)
			time.Sleep(1 * time.Second)
		}

		// 4. Delete each queue
		if _, err := ch.QueueDelete(queue.QueueName, false, false, false); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			msg := fmt.Sprintf("Failed to delete queue %s: %v", queue.QueueName, err)
			logger.Error(msg)
			return fmt.Errorf("%s", msg)
		}
		msg := fmt.Sprintf("Queue %s deleted", queue.QueueName)
		logger.Info(msg)
	}

	// 4. Remove service and notify
	if err := Services.RemoveService(req.ServiceId); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		msg := fmt.Sprintf("Failed to remove service %s: %v", req.ServiceId, err)
		logger.Error(msg)
		return fmt.Errorf("%s", msg)
	}
	msg := fmt.Sprintf("Service %s removed", req.ServiceId)
	logger.Info(msg)

	// 5. Service can now shut down

	span.SetStatus(codes.Ok, "")
	w.WriteHeader(http.StatusOK)
	return nil
}

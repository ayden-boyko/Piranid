package models

import (
	"fmt"
	"sync"

	transactions "github.com/ayden-boyko/Piranid/nodes/Event_Queue/transactions"

	"github.com/ayden-boyko/Piranid/nodes/Event_Queue/utils"
	amqp "github.com/rabbitmq/amqp091-go"
)

type Services struct {
	services map[string]*Service // keyed by uuid
	mu       sync.RWMutex
}

func (s *Services) SetServiceDraining(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	service := s.services[id]
	return service.SetServiceDraining()
}

func (s *Services) GetServices() map[string]*Service {
	return s.services
}

func NewServices() *Services {
	return &Services{services: make(map[string]*Service), mu: sync.RWMutex{}}
}

func (s *Services) AddServiceQueue(req *transactions.AddQueueRequest, service *Service) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// create new queue using service channel
	newQueue, err := service.Channel.QueueDeclare(
		req.QueueName,
		*req.Durable,
		*req.AutoDelete,
		*req.Exclusive,
		*req.NoWait,
		utils.MapToTable(req.Args),
	)
	if err != nil {
		return err
	}

	serviceQueue, err := createServiceQueue(req.ServiceId, req.QueueName, req.Loggable, req.Tags, &newQueue)
	if err != nil {
		return err
	}

	service.Queues[req.QueueName] = serviceQueue
	return nil
}

func (s *Services) GetService(serviceId string) (*Service, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// check if Service exists
	if _, ok := s.services[serviceId]; !ok {
		return nil, fmt.Errorf("Service %s does not exist", serviceId)
	}
	return s.services[serviceId], nil
}

func (s *Services) RemoveService(serviceId string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// check if Service exists
	if _, ok := s.services[serviceId]; !ok {
		return fmt.Errorf("Service %s does not exist", serviceId)
	}
	if service, err := s.GetService(serviceId); service.IsDraining || err != nil {
		return fmt.Errorf("Unable to remove service, currently draining")
	}
	delete(s.services, serviceId)
	return nil
}

func (s *Services) AddService(req *transactions.AddServiceRequest, channel *amqp.Channel) (*Service, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// check if Service exists
	if _, ok := s.services[req.ServiceId]; ok {
		return nil, fmt.Errorf("Service %s already exists", req.ServiceId)
	}
	// create new service
	newService, err := createService(req.ServiceId, channel)
	if err != nil {
		return nil, err
	}
	s.services[req.ServiceId] = newService
	return newService, nil
}

func (s *Services) GetServiceQueue(serviceId string, name string) (*ServiceQueue, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// check if queues related to serviceId exist
	if _, ok := s.services[serviceId]; !ok {
		return nil, fmt.Errorf("service queue for serviceId %s does not exist", serviceId)
	}
	return s.services[serviceId].Queues[name], nil
}

func (s *Services) RemoveServiceQueue(serviceId string, queuename string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// check if Service exists
	if _, ok := s.services[serviceId]; !ok {
		return fmt.Errorf("Service for serviceId %s does not exist", serviceId)
	}
	// check if queue exists
	queue, ok := s.services[serviceId].Queues[queuename]
	if !ok {
		return fmt.Errorf("Service Queue %s does not exist", queuename)
	}
	if queue.SearchTags("draining") {
		return fmt.Errorf("Unable to remove Service Queue, currently draining")
	}
	delete(s.services[serviceId].Queues, queuename)
	return nil
}

func (s *Services) GetAllServices() map[string]*Service {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.GetServices()
}

func createService(serviceId string, channel *amqp.Channel) (*Service, error) {
	newService := &Service{
		ServiceId: serviceId,
		Channel:   channel,
		Queues:    make(map[string]*ServiceQueue),
	}
	return newService, nil
}

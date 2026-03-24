package models

import (
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

type Service struct {
	ServiceId  string
	IsDraining bool
	Channel    *amqp.Channel
	Queues     map[string]*ServiceQueue
}

func (s *Service) SetServiceDraining() error {
	s.IsDraining = true
	return nil
}

func (s *Service) SetQueueDraining(name string) error {
	queue := s.Queues[name]
	isDraining := queue.SearchTags("draining")
	if isDraining {
		return fmt.Errorf("Already Draining Queue")
	} else {
		queue.Tags = append(queue.Tags, "draining")
	}
	return nil
}

func (s *Service) RemoveQueue(name string) error {
	before := s.Queues[name]
	if before.SearchTags("draining") {
		return fmt.Errorf("Unable to Remove Queue, currently draining")
	}
	delete(s.Queues, name)
	after := s.Queues[name]
	if before == after {
		return fmt.Errorf("Unable to remove queue: %s", name)
	}
	return nil
}

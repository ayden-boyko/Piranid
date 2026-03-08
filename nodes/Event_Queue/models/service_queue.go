package models

import (
	amqp "github.com/rabbitmq/amqp091-go"
)

type ServiceQueue struct {
	ServiceId string
	QueueName string
	Loggable  bool
	Tags      []string
	Queue     *amqp.Queue
}

// internal methods for creating EMPTY service queues
func createServiceQueue(serviceId string, name string, loggable bool, tags []string, queue *amqp.Queue) (*ServiceQueue, error) {
	newService := &ServiceQueue{
		QueueName: name,
		ServiceId: serviceId,
		Loggable:  loggable,
		Tags:      tags,
		Queue:     queue,
	}
	return newService, nil
}

func (s *ServiceQueue) SearchTags(tag string) bool {
	for _, t := range s.Tags {
		if tag == t {
			return true
		}
	}
	return false
}

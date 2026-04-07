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
	// create a copy of the tags slice to avoid unintended side effects
	tag_slice := make([]string, len(tags))
	copy(tag_slice, tags)
	// not coping can mutate the original slice,
	// which can cause unintended side effects if the original slice is used elsewhere in the code
	newService := &ServiceQueue{
		QueueName: name,
		ServiceId: serviceId,
		Loggable:  loggable,
		Tags:      tag_slice,
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

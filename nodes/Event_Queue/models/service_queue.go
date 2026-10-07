package models

import (
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

// ServiceQueue describes one queue registered against a service.
type ServiceQueue struct {
	ServiceId string
	QueueName string
	Loggable  bool
	Tags      []string

	// Queue holds the broker's reply to the declaration. It is retained for
	// reference only and is not read by any handler.
	Queue *amqp.Queue
}

// newServiceQueue builds a queue record from a declaration reply.
//
// The tags slice is copied rather than aliased, so a caller reusing its slice
// cannot mutate registry state behind the lock.
func newServiceQueue(serviceId, name string, loggable bool, tags []string, queue *amqp.Queue) *ServiceQueue {
	tagCopy := make([]string, len(tags))
	copy(tagCopy, tags)

	return &ServiceQueue{
		QueueName: name,
		ServiceId: serviceId,
		Loggable:  loggable,
		Tags:      tagCopy,
		Queue:     queue,
	}
}

// SearchTags reports whether the queue carries a tag.
func (s *ServiceQueue) SearchTags(tag string) bool {
	for _, t := range s.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

// snapshot returns a copy safe to hand to a caller outside the lock.
func (s *ServiceQueue) snapshot() ServiceQueue {
	out := *s
	out.Tags = make([]string, len(s.Tags))
	copy(out.Tags, s.Tags)
	return out
}

// QueueDeclaration carries the parameters of a queue declaration.
//
// The AMQP boolean flags are explicit bools rather than the *bool the previous
// DTO used. A *bool cannot distinguish "caller did not say" from "caller said
// false", which meant the handler had to guess a default while dereferencing a
// pointer that a client could omit entirely.
type QueueDeclaration struct {
	Name       string
	Durable    bool
	AutoDelete bool
	Exclusive  bool
	NoWait     bool
	Loggable   bool
	Tags       []string
	Args       map[string]string
}

// declareQueue creates a queue on the broker and records it in the service.
//
// Returns a snapshot, so the caller never holds a pointer into registry state.
func (s *Service) declareQueue(req QueueDeclaration) (ServiceQueue, error) {
	if s.Channel == nil {
		return ServiceQueue{}, fmt.Errorf("service %s has no open AMQP channel", s.ServiceId)
	}

	declared, err := s.Channel.QueueDeclare(
		req.Name,
		req.Durable,
		req.AutoDelete,
		req.Exclusive,
		req.NoWait,
		typedTable(req.Args),
	)
	if err != nil {
		return ServiceQueue{}, fmt.Errorf("declaring queue %s: %w", req.Name, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	queue := newServiceQueue(s.ServiceId, req.Name, req.Loggable, req.Tags, &declared)
	s.Queues[req.Name] = queue
	return queue.snapshot(), nil
}

// InjectQueueForTest registers a queue record without contacting a broker.
//
// It exists so registry and handler tests can exercise the read paths, which
// otherwise cannot run without a live AMQP channel. Production code has no use
// for it: registering a queue requires declaring it, which is what
// declareQueue does.
func InjectQueueForTest(s *Service, name string, tags ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Queues[name] = newServiceQueue(s.ServiceId, name, false, tags, nil)
}

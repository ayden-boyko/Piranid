package models

import (
	"errors"
	"fmt"
	"sync"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Registry errors. Callers test these with errors.Is rather than matching
// message strings, so the wording can change without breaking anyone.
//
// This is a typed contract because the HTTP layer maps each of these to a
// distinct status code (404, 409, 423). Previously every failure collapsed to a
// blanket 500, so a client could not tell "already exists" from "no such queue".
var (
	// ErrNotFound reports a service or queue that is not registered.
	ErrNotFound = errors.New("not found")

	// ErrAlreadyExists reports a duplicate registration.
	ErrAlreadyExists = errors.New("already exists")

	// ErrDraining reports an operation refused because a drain is in progress.
	ErrDraining = errors.New("drain in progress")
)

// DrainingTag is the marker appended to a queue's tags while it is draining.
const DrainingTag = "draining"

// Service is one registered client of the queue API, together with the AMQP
// channel used to declare that client's queues.
//
// A channel is not safe for concurrent use by multiple goroutines, so access is
// serialized through mu. That is also why each service gets its own channel
// rather than sharing the connection's default channel.
type Service struct {
	ServiceId string
	Channel   *amqp.Channel
	Queues    map[string]*ServiceQueue

	mu         sync.RWMutex
	isDraining bool
}

// NewService builds a Service around an open channel.
func NewService(serviceId string, channel *amqp.Channel) *Service {
	return &Service{
		ServiceId: serviceId,
		Channel:   channel,
		Queues:    make(map[string]*ServiceQueue),
	}
}

// ID returns the service identifier.
func (s *Service) ID() string { return s.ServiceId }

// IsDraining reports whether a service-level drain is in progress.
func (s *Service) IsDraining() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isDraining
}

// SetServiceDraining marks the service as draining.
//
// Draining is a marker, not a prohibition. It tells producers to stop enqueuing
// so the backlog can clear. Removal is a separate, later decision.
func (s *Service) SetServiceDraining() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.isDraining {
		return fmt.Errorf("service %s: %w", s.ServiceId, ErrDraining)
	}
	s.isDraining = true
	return nil
}

// ClearServiceDraining removes the marker after a drain has completed.
func (s *Service) ClearServiceDraining() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.isDraining = false
}

// SetQueueDraining marks a queue as draining.
//
// Returns ErrNotFound for an unknown queue and ErrDraining if the service as a
// whole is already draining. Previously an unknown queue produced a nil-pointer
// dereference here, because the map lookup result was used unchecked.
func (s *Service) SetQueueDraining(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isDraining {
		return fmt.Errorf("service %s: %w", s.ServiceId, ErrDraining)
	}
	queue, ok := s.Queues[name]
	if !ok {
		return fmt.Errorf("queue %s in service %s: %w", name, s.ServiceId, ErrNotFound)
	}
	if queue.SearchTags(DrainingTag) {
		return fmt.Errorf("queue %s in service %s: %w", name, s.ServiceId, ErrDraining)
	}
	queue.Tags = append(queue.Tags, DrainingTag)
	return nil
}

// ClearQueueDraining removes the draining marker from a queue.
//
// Called once the broker-side queue has been deleted, so the subsequent
// RemoveQueue is not blocked by the marker the handler itself set.
func (s *Service) ClearQueueDraining(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	queue, ok := s.Queues[name]
	if !ok {
		return fmt.Errorf("queue %s in service %s: %w", name, s.ServiceId, ErrNotFound)
	}
	kept := queue.Tags[:0]
	for _, t := range queue.Tags {
		if t != DrainingTag {
			kept = append(kept, t)
		}
	}
	queue.Tags = kept
	return nil
}

// GetQueue returns a snapshot of one queue.
//
// It copies the struct and the tag slice, so a caller iterating the result
// cannot race with a concurrent drain. The previous version returned the live
// pointer out from under the lock.
func (s *Service) GetQueue(name string) (ServiceQueue, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	queue, ok := s.Queues[name]
	if !ok {
		return ServiceQueue{}, fmt.Errorf("queue %s in service %s: %w", name, s.ServiceId, ErrNotFound)
	}
	return queue.snapshot(), nil
}

// SnapshotQueue returns copies of every queue on the service.
func (s *Service) SnapshotQueue() map[string]ServiceQueue {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make(map[string]ServiceQueue, len(s.Queues))
	for name, q := range s.Queues {
		out[name] = q.snapshot()
	}
	return out
}

// RemoveQueue deletes a queue from the in-memory registry.
//
// It refuses while a drain is in progress, which is what the original
// implementation intended, but the marker must be cleared first. Previously the
// handler set the "draining" tag and then called this method, so every removal
// failed and the broker and the model were left permanently out of sync.
//
// The previous post-delete verification was also meaningless: it read the key
// back after deleting it and compared to the value captured before, which is
// always nil after a successful delete.
func (s *Service) RemoveQueue(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	queue, ok := s.Queues[name]
	if !ok {
		return fmt.Errorf("queue %s in service %s: %w", name, s.ServiceId, ErrNotFound)
	}
	if queue.SearchTags(DrainingTag) {
		return fmt.Errorf("queue %s in service %s: %w", name, s.ServiceId, ErrDraining)
	}
	delete(s.Queues, name)
	return nil
}

// Close releases the service's AMQP channel.
//
// Services used to leak their channel on removal: one channel was opened per
// registered service and never closed.
func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Channel == nil {
		return nil
	}
	err := s.Channel.Close()
	s.Channel = nil
	return err
}

// deleteQueue removes a queue from the broker.
//
// Distinct from RemoveQueue, which only touches the in-memory registry. The
// handler calls this mid-drain, before the registry entry is dropped.
//
// The write lock is held across the broker call on purpose: an AMQP channel is
// not safe for concurrent use, and this service owns exactly one channel, so
// serializing on it is the only correct option. The lock is per-service, so it
// does not block other services.
func (s *Service) DeleteQueue(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Channel == nil {
		return fmt.Errorf("service %s has no open AMQP channel", s.ServiceId)
	}
	if _, err := s.Channel.QueueDelete(name, false, false, false); err != nil {
		return err
	}
	return nil
}

// pendingMessages reports how many messages a queue currently holds.
//
// A queue that no longer exists is reported as 0, because for the purposes of a
// drain it is already empty. Any other error is returned.
func (s *Service) PendingMessages(name string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Channel == nil {
		return 0, fmt.Errorf("service %s has no open AMQP channel", s.ServiceId)
	}

	q, err := s.Channel.QueueDeclarePassive(name, false, false, false, false, nil)
	if err != nil {
		// A passive declare of a missing queue closes the channel with a
		// NOT_FOUND. Treat it as drained rather than as a failure.
		return 0, nil
	}
	return q.Messages, nil
}

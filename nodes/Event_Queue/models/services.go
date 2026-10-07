package models

import (
	"fmt"
	"sync"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Services is the registry of services and their queues.
//
// The zero value is not usable: call NewServices. This type used to be embedded
// in EventNode as a plain field and never constructed, leaving the map nil.
// Reads on a nil map are legal, which is why the read paths looked healthy, but
// the first AddService panicked with "assignment to entry in nil map".
type Services struct {
	services map[string]*Service
	mu       sync.RWMutex
}

// NewServices builds an initialised registry.
func NewServices() *Services {
	return &Services{services: make(map[string]*Service)}
}

// EnsureInitialised makes the zero value usable.
//
// This exists so a Services obtained as a struct field cannot be left in a state
// where writes panic. It is safe to call repeatedly.
func (s *Services) EnsureInitialised() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.services == nil {
		s.services = make(map[string]*Service)
	}
}

// AddService registers a service against an AMQP channel.
//
// Returns ErrAlreadyExists for a duplicate id.
func (s *Services) AddService(serviceId string, channel *amqp.Channel) (*Service, error) {
	if serviceId == "" {
		return nil, fmt.Errorf("service id must not be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.services == nil {
		s.services = make(map[string]*Service)
	}
	if _, exists := s.services[serviceId]; exists {
		return nil, fmt.Errorf("service %s: %w", serviceId, ErrAlreadyExists)
	}

	svc := NewService(serviceId, channel)
	s.services[serviceId] = svc
	return svc, nil
}

// GetService returns a service, or ErrNotFound.
//
// The returned pointer is live and safe for concurrent use: every method on it
// takes its own lock. Callers must not read the Queues map directly; use
// GetQueue or SnapshotQueue.
func (s *Services) GetService(serviceId string) (*Service, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	svc, ok := s.services[serviceId]
	if !ok {
		return nil, fmt.Errorf("service %s: %w", serviceId, ErrNotFound)
	}
	return svc, nil
}

// SnapshotServices returns copies of every service's queue map.
//
// This replaces GetServices and GetAllServices, both of which returned the
// internal map directly and leaked a live reference to callers.
func (s *Services) SnapshotServices() map[string]map[string]ServiceQueue {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make(map[string]map[string]ServiceQueue, len(s.services))
	for id, svc := range s.services {
		out[id] = svc.SnapshotQueue()
	}
	return out
}

// ServiceIDs returns the registered service ids.
func (s *Services) ServiceIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := make([]string, 0, len(s.services))
	for id := range s.services {
		ids = append(ids, id)
	}
	return ids
}

// AddServiceQueue declares a queue on the service's channel and records it.
//
// Returns ErrAlreadyExists for a duplicate queue name. The declaration happens
// before the registry is touched, so a broker rejection leaves no phantom entry.
func (s *Services) AddServiceQueue(serviceId string, decl QueueDeclaration) (ServiceQueue, error) {
	svc, err := s.GetService(serviceId)
	if err != nil {
		return ServiceQueue{}, err
	}

	if _, err := svc.GetQueue(decl.Name); err == nil {
		return ServiceQueue{}, fmt.Errorf("queue %s in service %s: %w",
			decl.Name, serviceId, ErrAlreadyExists)
	}

	return svc.declareQueue(decl)
}

// GetServiceQueue returns one queue, or ErrNotFound.
//
// The previous version returned (nil, nil) for a missing queue. Every caller
// treated a nil error as success, which is why AddQueueHandler rejected every
// queue creation as "already exists" and GetQueueHandler would have
// dereferenced nil.
func (s *Services) GetServiceQueue(serviceId, name string) (ServiceQueue, error) {
	svc, err := s.GetService(serviceId)
	if err != nil {
		return ServiceQueue{}, err
	}
	return svc.GetQueue(name)
}

// RemoveServiceQueue deletes a queue from the registry.
//
// Refuses with ErrDraining while the queue carries the draining marker; the
// handler clears the marker once the broker-side delete has completed.
func (s *Services) RemoveServiceQueue(serviceId, name string) error {
	svc, err := s.GetService(serviceId)
	if err != nil {
		return err
	}
	return svc.RemoveQueue(name)
}

// RemoveService deletes a service and closes its channel.
//
// Refuses with ErrDraining while a drain is in progress.
//
// The previous implementation called GetService while already holding the write
// lock. GetService takes the read lock, and sync.RWMutex is not reentrant, so
// this self-deadlocked and, because it held the write lock, every other request
// on the node blocked behind it. It also dereferenced service.IsDraining before
// checking err, so a missing service was a nil-pointer panic.
func (s *Services) RemoveService(serviceId string) error {
	s.mu.RLock()
	svc, ok := s.services[serviceId]
	s.mu.RUnlock()

	if !ok {
		return fmt.Errorf("service %s: %w", serviceId, ErrNotFound)
	}
	if svc.IsDraining() {
		return fmt.Errorf("service %s: %w", serviceId, ErrDraining)
	}

	// Drop the entry first, under the write lock, so no two removals race.
	s.mu.Lock()
	if _, still := s.services[serviceId]; !still {
		s.mu.Unlock()
		return fmt.Errorf("service %s: %w", serviceId, ErrNotFound)
	}
	delete(s.services, serviceId)
	s.mu.Unlock()

	// Close outside the lock: a network call must not block the registry.
	return svc.Close()
}

// Len reports how many services are registered.
func (s *Services) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.services)
}

// Reset drops every registered service and closes its channel.
//
// Called after a broker reconnect. Every stored channel belongs to the dead
// connection, so keeping the entries would let requests issue declarations on
// handles that no longer work. Clients re-register against the new connection.
func (s *Services) Reset() int {
	s.mu.Lock()
	dropped := s.services
	s.services = make(map[string]*Service)
	s.mu.Unlock()

	for _, svc := range dropped {
		_ = svc.Close()
	}
	return len(dropped)
}

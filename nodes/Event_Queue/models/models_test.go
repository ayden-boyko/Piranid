package models

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// newRegistry builds an initialised registry.
//
// This is the state the shipped node never reached: Services was an
// unconstructed struct field, so its map was nil and the first AddService
// panicked with "assignment to entry in nil map".
func newRegistry() *Services {
	return NewServices()
}

func mustAddService(t *testing.T, s *Services, id string) *Service {
	t.Helper()
	svc, err := s.AddService(id, nil) // channel nil: registry logic only
	if err != nil {
		t.Fatalf("AddService(%q): %v", id, err)
	}
	return svc
}

// addQueueInjects a queue record without touching a broker.
func addQueueInjects(t *testing.T, svc *Service, name string, tags ...string) ServiceQueue {
	t.Helper()
	svc.mu.Lock()
	defer svc.mu.Unlock()
	q := newServiceQueue(svc.ServiceId, name, false, tags, nil)
	svc.Queues[name] = q
	return q.snapshot()
}

func TestRegistryIsInitialised(t *testing.T) {
	s := newRegistry()
	if s.services == nil {
		t.Fatal("NewServices left the map nil")
	}
	if got := s.Len(); got != 0 {
		t.Errorf("Len = %d, want 0", got)
	}
}

// A Services obtained as a struct field starts zero-valued. Writing to a nil
// map panics, which is what the shipped node did on its first AddService.
//
// AddService now allocates the map on demand, so even a zero value is safe.
func TestZeroValueAddServiceIsSafe(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("AddService on a zero-value Services panicked: %v", r)
		}
	}()

	var s Services
	if _, err := s.AddService("svc", nil); err != nil {
		t.Fatalf("AddService on a zero value: %v", err)
	}
	if s.Len() != 1 {
		t.Errorf("Len = %d, want 1", s.Len())
	}
}

// EnsureInitialised exists for callers that want the allocation to be explicit.
// It must be safe to call repeatedly, including on an already-populated registry.
func TestEnsureInitialisedIsIdempotent(t *testing.T) {
	var s Services
	s.EnsureInitialised()
	mustAddService(t, &s, "svc-1")

	s.EnsureInitialised()
	s.EnsureInitialised()

	if s.Len() != 1 {
		t.Errorf("EnsureInitialised disturbed existing entries: Len = %d, want 1", s.Len())
	}
}

func TestAddServiceDuplicateIsConflict(t *testing.T) {
	s := newRegistry()
	mustAddService(t, s, "svc-1")

	_, err := s.AddService("svc-1", nil)
	if !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("err = %v, want ErrAlreadyExists", err)
	}
}

func TestAddServiceRejectsEmptyID(t *testing.T) {
	s := newRegistry()
	if _, err := s.AddService("", nil); err == nil {
		t.Error("AddService accepted an empty id")
	}
}

func TestGetServiceMissingIsNotFound(t *testing.T) {
	s := newRegistry()
	_, err := s.GetService("nope")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// The previous RemoveService took the write lock and then called GetService,
// which takes the read lock. sync.RWMutex is not reentrant, so this
// self-deadlocked and, holding the write lock, blocked every other request.
func TestRemoveServiceDoesNotDeadlock(t *testing.T) {
	s := newRegistry()
	mustAddService(t, s, "svc-1")

	done := make(chan error, 1)
	go func() { done <- s.RemoveService("svc-1") }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RemoveService: %v", err)
		}
	case <-timeoutAfter():
		t.Fatal("RemoveService deadlocked: it took Lock and then requested RLock on the same mutex")
	}
}

func TestRemoveServiceMissingIsNotFound(t *testing.T) {
	s := newRegistry()
	err := s.RemoveService("nope")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// The previous RemoveService evaluated service.IsDraining before checking err,
// so a missing service was a nil-pointer dereference.
func TestRemoveServiceMissingDoesNotPanic(t *testing.T) {
	s := newRegistry()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("RemoveService panicked on a missing service: %v", r)
		}
	}()
	_ = s.RemoveService("nope")
}

func TestRemoveServiceRefusesWhileDraining(t *testing.T) {
	s := newRegistry()
	svc := mustAddService(t, s, "svc-1")
	if err := svc.SetServiceDraining(); err != nil {
		t.Fatalf("SetServiceDraining: %v", err)
	}

	if err := s.RemoveService("svc-1"); !errors.Is(err, ErrDraining) {
		t.Errorf("err = %v, want ErrDraining", err)
	}

	svc.ClearServiceDraining()
	if err := s.RemoveService("svc-1"); err != nil {
		t.Errorf("RemoveService after clearing the marker: %v", err)
	}
}

// GetServiceQueue returned (nil, nil) for a missing queue. Every caller treated
// a nil error as success, which is why AddQueueHandler rejected every queue
// creation as "already exists".
func TestGetServiceQueueMissingIsNotFound(t *testing.T) {
	s := newRegistry()
	svc := mustAddService(t, s, "svc-1")

	_, err := s.GetServiceQueue("svc-1", "never-created")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound (was (nil, nil))", err)
	}
	_ = svc
}

func TestGetServiceQueueExisting(t *testing.T) {
	s := newRegistry()
	svc := mustAddService(t, s, "svc-1")
	addQueueInjects(t, svc, "q1", "blue")

	got, err := s.GetServiceQueue("svc-1", "q1")
	if err != nil {
		t.Fatalf("GetServiceQueue: %v", err)
	}
	if got.QueueName != "q1" {
		t.Errorf("QueueName = %q, want q1", got.QueueName)
	}
	if !got.SearchTags("blue") {
		t.Errorf("tags = %v, want to contain blue", got.Tags)
	}
}

// The drain protocol marked a queue "draining" and then called a method that
// refused anything so marked, so every removal failed and the broker and the
// model diverged permanently. This exercises the corrected sequence.
func TestDrainThenRemoveSequenceSucceeds(t *testing.T) {
	s := newRegistry()
	svc := mustAddService(t, s, "svc-1")
	addQueueInjects(t, svc, "q1")

	// Step 1: mark draining.
	if err := svc.SetQueueDraining("q1"); err != nil {
		t.Fatalf("SetQueueDraining: %v", err)
	}
	if err := s.RemoveServiceQueue("svc-1", "q1"); !errors.Is(err, ErrDraining) {
		t.Errorf("removal during drain = %v, want ErrDraining", err)
	}

	// Step 2: after the broker delete, clear the marker.
	if err := svc.ClearQueueDraining("q1"); err != nil {
		t.Fatalf("ClearQueueDraining: %v", err)
	}

	// Step 3: remove from the registry.
	if err := s.RemoveServiceQueue("svc-1", "q1"); err != nil {
		t.Fatalf("RemoveServiceQueue after the drain: %v", err)
	}

	if _, err := s.GetServiceQueue("svc-1", "q1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("queue still present after removal: %v", err)
	}
}

func TestSetQueueDrainingUnknownQueue(t *testing.T) {
	s := newRegistry()
	svc := mustAddService(t, s, "svc-1")

	// The previous version dereferenced the map lookup result unchecked.
	err := svc.SetQueueDraining("ghost")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestSetQueueDrainingTwiceIsDraining(t *testing.T) {
	s := newRegistry()
	svc := mustAddService(t, s, "svc-1")
	addQueueInjects(t, svc, "q1")

	if err := svc.SetQueueDraining("q1"); err != nil {
		t.Fatalf("first SetQueueDraining: %v", err)
	}
	if err := svc.SetQueueDraining("q1"); !errors.Is(err, ErrDraining) {
		t.Errorf("second SetQueueDraining = %v, want ErrDraining", err)
	}
}

func TestClearQueueDrainingPreservesOtherTags(t *testing.T) {
	s := newRegistry()
	svc := mustAddService(t, s, "svc-1")
	addQueueInjects(t, svc, "q1", "blue", "green")

	if err := svc.SetQueueDraining("q1"); err != nil {
		t.Fatalf("SetQueueDraining: %v", err)
	}
	if err := svc.ClearQueueDraining("q1"); err != nil {
		t.Fatalf("ClearQueueDraining: %v", err)
	}

	q, err := svc.GetQueue("q1")
	if err != nil {
		t.Fatalf("GetQueue: %v", err)
	}
	if q.SearchTags(DrainingTag) {
		t.Errorf("tags = %v, still draining", q.Tags)
	}
	if !q.SearchTags("blue") || !q.SearchTags("green") {
		t.Errorf("tags = %v, want blue and green preserved", q.Tags)
	}
}

// Snapshots must be independent copies, or a caller mutating one races the
// registry. The previous code returned the live pointer and the live map.
func TestSnapshotQueueIsIndependent(t *testing.T) {
	s := newRegistry()
	svc := mustAddService(t, s, "svc-1")
	addQueueInjects(t, svc, "q1", "blue")

	snap := svc.SnapshotQueue()
	snap["q1"].Tags[0] = "tampered"

	again := svc.SnapshotQueue()
	if again["q1"].Tags[0] != "blue" {
		t.Errorf("registry was mutated through a snapshot: %v", again["q1"].Tags)
	}
}

func TestSnapshotServicesIsIndependent(t *testing.T) {
	s := newRegistry()
	svc := mustAddService(t, s, "svc-1")
	addQueueInjects(t, svc, "q1", "blue")

	snap := s.SnapshotServices()
	delete(snap, "svc-1")

	if s.Len() != 1 {
		t.Errorf("registry was mutated through SnapshotServices: Len = %d", s.Len())
	}
	_ = svc
}

func TestResetDropsServices(t *testing.T) {
	s := newRegistry()
	mustAddService(t, s, "a")
	mustAddService(t, s, "b")

	if n := s.Reset(); n != 2 {
		t.Errorf("Reset returned %d, want 2", n)
	}
	if s.Len() != 0 {
		t.Errorf("Len after Reset = %d, want 0", s.Len())
	}
	// Must remain usable.
	mustAddService(t, s, "c")
}

// Concurrent registry access must be race-free. GetService previously returned
// the live pointer and handlers iterated service.Queues with no lock held.
func TestConcurrentRegistryAccess(t *testing.T) {
	s := newRegistry()
	svc := mustAddService(t, s, "svc-1")
	for _, name := range []string{"q1", "q2", "q3"} {
		addQueueInjects(t, svc, name)
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Readers
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = s.SnapshotServices()
					_ = svc.SnapshotQueue()
				}
			}
		}()
	}

	// Writers
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = svc.SetQueueDraining("q1")
				_ = svc.ClearQueueDraining("q1")
			}
		}
	}()

	// Give the goroutines a moment, then stop.
	for i := 0; i < 2000; i++ {
		s.SnapshotServices()
	}
	close(stop)
	wg.Wait()
}

func TestServiceIDs(t *testing.T) {
	s := newRegistry()
	mustAddService(t, s, "a")
	mustAddService(t, s, "b")

	ids := s.ServiceIDs()
	if len(ids) != 2 {
		t.Fatalf("ServiceIDs returned %d entries, want 2", len(ids))
	}
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	if !seen["a"] || !seen["b"] {
		t.Errorf("ServiceIDs = %v, want to contain a and b", ids)
	}
}

// timeoutAfter bounds the deadlock assertion.
func timeoutAfter() <-chan time.Time { return time.After(2 * time.Second) }

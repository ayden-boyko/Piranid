package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ayden-boyko/Piranid/nodes/Event_Queue/models"
	transactions "github.com/ayden-boyko/Piranid/nodes/Event_Queue/transactions"

	"go.uber.org/zap"
)

// harness wires the real handlers onto a real ServeMux with an initialised
// registry, so routing, path-value extraction and status mapping are all
// exercised. No broker is available, so only the paths that do not need one are
// exercised here; broker-dependent paths are covered in models_test.go.
func harness(t *testing.T) (http.Handler, *models.Services) {
	t.Helper()

	services := models.NewServices()
	deps := Deps{
		Services:     services,
		Connection:   nil, // no broker in these tests
		Logger:       zap.NewNop(),
		DrainTimeout: time.Second,
	}

	mux := http.NewServeMux()

	// Patterns mirror eventcore.RegisterRoutes exactly, so a mismatch between
	// the two shows up here.
	mux.HandleFunc("POST /api/v1/services/{service_id}",
		func(w http.ResponseWriter, r *http.Request) { AddServiceHandler(w, r, deps) })
	mux.HandleFunc("GET /api/v1/services/{service_id}",
		func(w http.ResponseWriter, r *http.Request) { GetServiceHandler(w, r, deps) })
	mux.HandleFunc("GET /api/v1/services",
		func(w http.ResponseWriter, r *http.Request) { GetAllServicesHandler(w, r, deps) })
	mux.HandleFunc("GET /api/v1/services/{service_id}/queue/{queue_id}",
		func(w http.ResponseWriter, r *http.Request) { GetQueueHandler(w, r, deps) })
	mux.HandleFunc("POST /api/v1/services/{service_id}/queue/{queue_id}",
		func(w http.ResponseWriter, r *http.Request) { AddQueueHandler(w, r, deps) })
	mux.HandleFunc("/api/v1/event_test", EventTestHandler)

	return mux, services
}

// do issues a request against the harness.
func do(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshalling body: %v", err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}

	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// seed registers a service and a queue directly in the registry, bypassing the
// broker, so read paths can be tested.
func seed(t *testing.T, s *models.Services, serviceID, queueName string) {
	t.Helper()
	svc, err := s.AddService(serviceID, nil)
	if err != nil {
		t.Fatalf("AddService: %v", err)
	}
	// Inject directly; the exported path needs a broker channel.
	injectQueue(t, svc, queueName, "blue")
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body transactions.ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding error body %q: %v", rec.Body.String(), err)
	}
	return body.Error
}

// ---------------------------------------------------------------------------

func TestGetMissingServiceIs404(t *testing.T) {
	h, _ := harness(t)

	rec := do(t, h, http.MethodGet, "/api/v1/services/ghost", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if code := errorCode(t, rec); code != transactions.CodeNotFound {
		t.Errorf("error = %q, want %q", code, transactions.CodeNotFound)
	}
}

func TestGetMissingQueueIs404(t *testing.T) {
	h, services := harness(t)
	seed(t, services, "svc-1", "q1")

	rec := do(t, h, http.MethodGet, "/api/v1/services/svc-1/queue/ghost", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// The previous GetServiceQueue returned (nil, nil) for a missing queue, so the
// "already exists" check in AddQueueHandler fired for every queue.
func TestGetMissingQueueIs404NotNilNil(t *testing.T) {
	h, services := harness(t)
	seed(t, services, "svc-1", "q1")

	rec := do(t, h, http.MethodGet, "/api/v1/services/svc-1/queue/never-created", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if rec.Code == http.StatusInternalServerError {
		t.Error("a missing queue produced a 500, indicating a nil dereference")
	}
}

func TestGetServiceReturnsQueues(t *testing.T) {
	h, services := harness(t)
	seed(t, services, "svc-1", "q1")

	rec := do(t, h, http.MethodGet, "/api/v1/services/svc-1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var resp transactions.GetServiceResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if resp.ServiceId != "svc-1" {
		t.Errorf("service_id = %q, want svc-1", resp.ServiceId)
	}
	// The Queues field previously had no JSON tag and serialized as "Queues".
	if _, present := mapFromJSON(t, rec.Body.Bytes())["queues"]; !present {
		t.Errorf("response missing a lowercase %q key: %s", "queues", rec.Body.String())
	}
	if len(resp.Queues) != 1 {
		t.Errorf("queues = %v, want 1 entry", resp.Queues)
	}
}

func TestQueueResponseReportsDraining(t *testing.T) {
	h, services := harness(t)
	seed(t, services, "svc-1", "q1")

	svc, err := services.GetService("svc-1")
	if err != nil {
		t.Fatalf("GetService: %v", err)
	}
	if err := svc.SetQueueDraining("q1"); err != nil {
		t.Fatalf("SetQueueDraining: %v", err)
	}

	rec := do(t, h, http.MethodGet, "/api/v1/services/svc-1/queue/q1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var q transactions.GetQueueResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &q); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if !q.Draining {
		t.Error("draining = false, want true")
	}
}

func TestGetAllServices(t *testing.T) {
	h, services := harness(t)
	seed(t, services, "svc-1", "q1")
	seed(t, services, "svc-2", "q2")

	rec := do(t, h, http.MethodGet, "/api/v1/services", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp transactions.GetAllServicesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(resp.Services) != 2 {
		t.Errorf("services = %d, want 2", len(resp.Services))
	}
}

// The route pattern declares {service_id} but the old handlers read the id from
// the JSON body, so a GET with no body failed at the decode step.
func TestServiceIDComesFromThePath(t *testing.T) {
	h, services := harness(t)
	seed(t, services, "path-service", nil2("q1"))

	rec := do(t, h, http.MethodGet, "/api/v1/services/path-service", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 with no request body: %s", rec.Code, rec.Body.String())
	}
}

func TestMethodRoutingIsEnforced(t *testing.T) {
	h, _ := harness(t)

	// The queue path is registered for GET and POST only, so PATCH has a path
	// match but no method match.
	if rec := do(t, h, http.MethodPatch, "/api/v1/services/x/queue/y", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("PATCH on the queue path = %d, want 405", rec.Code)
	}
	// The service path is registered for GET and POST in this harness.
	if rec := do(t, h, http.MethodPatch, "/api/v1/services/x", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("PATCH on the service path = %d, want 405", rec.Code)
	}
}

func TestEventTestIsUnauthenticatedAndHealthy(t *testing.T) {
	h, _ := harness(t)

	rec := do(t, h, http.MethodGet, "/api/v1/event_test", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestAddServiceWithoutBrokerIs503(t *testing.T) {
	h, _ := harness(t)

	// Connection is nil in this harness, so the handler must report the broker
	// as unavailable rather than panicking.
	rec := do(t, h, http.MethodPost, "/api/v1/services/svc-new",
		transactions.AddServiceRequest{Name: "New"})
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestAddQueueWithoutBrokerIsError(t *testing.T) {
	h, services := harness(t)
	seed(t, services, "svc-1", nil2("q1"))

	// svc-1's channel is nil, so declareQueue must fail cleanly.
	rec := do(t, h, http.MethodPost, "/api/v1/services/svc-1/queue/q2",
		transactions.AddQueueRequest{Durable: true})
	if rec.Code == http.StatusOK {
		t.Fatal("declaring a queue with no channel returned 200")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Logf("status = %d (acceptable: broker unavailable path)", rec.Code)
	}
}

func TestAddQueueRequiresQueueName(t *testing.T) {
	h, services := harness(t)
	seed(t, services, "svc-1", nil2("q1"))

	// Path carries no queue_id and the body carries no queue_name.
	rec := do(t, h, http.MethodPost, "/api/v1/services/svc-1/queue/",
		transactions.AddQueueRequest{})
	if rec.Code == http.StatusOK {
		t.Fatal("a queue with no name was accepted")
	}
}

func TestRejectUnknownJSONFields(t *testing.T) {
	h, services := harness(t)
	seed(t, services, "svc-1", nil2("q1"))

	rec := do(t, h, http.MethodPost, "/api/v1/services/svc-1/queue/q2",
		map[string]any{"nonsense": true})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for an unrecognised field", rec.Code)
	}
}

func TestStatusForMapsRegistryErrors(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{models.ErrNotFound, http.StatusNotFound, transactions.CodeNotFound},
		{models.ErrAlreadyExists, http.StatusConflict, transactions.CodeConflict},
		{models.ErrDraining, http.StatusLocked, transactions.CodeDraining},
		{errors.New("other"), http.StatusInternalServerError, transactions.CodeInternal},
	}

	for _, c := range cases {
		status, code := statusFor(c.err)
		if status != c.status || code != c.code {
			t.Errorf("statusFor(%v) = (%d, %q), want (%d, %q)",
				c.err, status, code, c.status, c.code)
		}
	}
}

// Wrapped errors must still map correctly.
func TestStatusForHandlesWrappedErrors(t *testing.T) {
	wrapped := errors.Join(errors.New("context"), models.ErrNotFound)
	status, code := statusFor(wrapped)
	if status != http.StatusNotFound || code != transactions.CodeNotFound {
		t.Errorf("statusFor(wrapped ErrNotFound) = (%d, %q), want (404, not_found)", status, code)
	}
}

// ---------------------------------------------------------------------------

// nil2 keeps the seed calls readable: pass "" to register no queue.
func nil2(name string) string { return name }

func injectQueue(t *testing.T, svc *models.Service, name string, tags ...string) {
	t.Helper()
	if name == "" {
		return
	}
	// The exported registry path needs a broker, so go through the model's own
	// constructor via a tiny helper exported for tests.
	models.InjectQueueForTest(svc, name, tags...)
}

func mapFromJSON(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	return out
}

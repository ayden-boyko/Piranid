package internal

// ErrorResponse is the uniform error body for every non-2xx response.
//
// Handlers previously returned bare errors to the route closures, which mapped
// all of them to a plain-text 500. A client could not tell "already exists" from
// "no such queue" from "a drain is in progress".
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

// Error codes. These are stable strings; clients may switch on them.
const (
	CodeNotFound       = "not_found"
	CodeAlreadyExists  = "already_exists"
	CodeDraining       = "draining"
	CodeInvalidRequest = "invalid_request"
	CodeBadRequest     = "bad_request"
	CodeBrokerError    = "broker_error"
	CodeInternal       = "internal_error"
	CodeDrainTimeout   = "drain_timeout"
	CodeConflict       = "conflict"
)

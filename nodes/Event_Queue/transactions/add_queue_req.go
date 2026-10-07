package internal

// AddQueueRequest declares a queue for a service.
//
// The AMQP boolean flags are plain bools. They were *bool before, which could
// not distinguish "the caller did not say" from "the caller said false", and a
// client omitting the field entirely left the handler dereferencing nil.
//
// Defaults when the field is absent: Durable=false, AutoDelete=false,
// Exclusive=false, NoWait=false.
type AddQueueRequest struct {
	ServiceId  string            `json:"service_id,omitempty"`
	QueueName  string            `json:"queue_name,omitempty"`
	Loggable   bool              `json:"loggable"`
	Tags       []string          `json:"tags,omitempty"`
	Durable    bool              `json:"durable"`
	AutoDelete bool              `json:"auto_delete"`
	Exclusive  bool              `json:"exclusive"`
	NoWait     bool              `json:"no_wait"`
	Args       map[string]string `json:"args,omitempty"`
}

package internal

// AddServiceRequest registers a service and opens its AMQP channel.
//
// ServiceId is normally taken from the URL path rather than the body. It is
// retained as an optional body field so a client that cannot set a path segment
// still works; when both are present the path wins.
type AddServiceRequest struct {
	ServiceId string   `json:"service_id,omitempty"`
	Name      string   `json:"name,omitempty"`
	Loggable  bool     `json:"loggable"`
	Tags      []string `json:"tags,omitempty"`
}

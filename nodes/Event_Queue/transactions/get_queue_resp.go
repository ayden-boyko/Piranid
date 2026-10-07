package internal

// GetQueueResponse describes one registered queue.
type GetQueueResponse struct {
	Name      string   `json:"name"`
	ServiceId string   `json:"service_id"`
	Loggable  bool     `json:"loggable"`
	Tags      []string `json:"tags"`
	Draining  bool     `json:"draining"`
}

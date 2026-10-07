package internal

// GetServiceResponse describes a service and its queues.
type GetServiceResponse struct {
	ServiceId string                      `json:"service_id"`
	Queues    map[string]GetQueueResponse `json:"queues"`
}

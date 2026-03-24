package internal

type GetServiceResponse struct {
	ServiceId string `json:"service_id"`
	Queues    map[string]GetQueueResponse
}

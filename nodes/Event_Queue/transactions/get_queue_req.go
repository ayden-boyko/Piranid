package internal

// returns queues for a given service
type GetQueueRequest struct {
	ServiceId string `json:"service_id"`
	QueueName string `json:"queue_name"`
}

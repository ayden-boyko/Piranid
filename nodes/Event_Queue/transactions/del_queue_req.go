package internal

type RemoveQueueRequest struct {
	ServiceId string `json:"service_id"`
	QueueName string `json:"queue_name"`
}

package internal

type AddQueueRequest struct {
	ServiceId string `json:"service_id"`
	QueueId   string `json:"queue_id"`
}

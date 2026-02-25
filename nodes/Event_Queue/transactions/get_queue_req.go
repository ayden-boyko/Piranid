package internal

// returns queues for a given service
type GetQueueRequest struct {
	ServiceId string `json:"service_id"`
}

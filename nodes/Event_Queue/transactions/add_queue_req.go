package internal

type AddQueueRequest struct {
	ServiceId string   `json:"service_id"`
	QueueName string   `json:"queue_name"`
	Loggable  bool     `json:"loggable"`
	Tags      []string `json:"tags"`
	// pointers cause it means the field is optional
	Durable    *bool             `json:"durable,omitempty"`
	AutoDelete *bool             `json:"auto_delete,omitempty"`
	Exclusive  *bool             `json:"exclusive,omitempty"`
	NoWait     *bool             `json:"no_wait,omitempty"`
	Args       map[string]string `json:"args,omitempty"`
}

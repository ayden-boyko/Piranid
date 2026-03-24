package internal

type AddServiceRequest struct {
	ServiceId string   `json:"service_id"`
	Name      string   `json:"name"`
	Loggable  bool     `json:"loggable"`
	Tags      []string `json:"tags"`
}

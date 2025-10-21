package internal

type ConsentPage struct {
	Destination  string `json:"destination"`
	ClientID     string `json:"client_id"`
	ResponseType string `json:"response_type"`
	Redirect     string `json:"redirect"`
}

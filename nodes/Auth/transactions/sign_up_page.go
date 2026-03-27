package internal

type SignUpPage struct {
	ClientID     string `json:"client_id"`
	ClientName   string `json:"client_name"`
	ServiceID    string `json:"service_id"`
	RedirectURI  string `json:"redirect_uri"`
	ResponseType string `json:"response_type"`
}

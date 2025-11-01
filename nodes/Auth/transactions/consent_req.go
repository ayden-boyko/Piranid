package internal

type ConsentPage struct {
	ClientID     string `json:"client_id"`
	ClientName   string `json:"client_name"`
	ResponseType string `json:"response_type"`
	RedirectURI  string `json:"redirect_uri"`
}

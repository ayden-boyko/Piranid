package internal

type AuthExchange struct {
	Authcode     string `json:"authcode"`
	Responsetype string `json:"response_type"`
	ClientId     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Redirect     string `json:"redirect"`
}

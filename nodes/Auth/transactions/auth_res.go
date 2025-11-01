package internal

type AuthResponse struct {
	Authcode    string `json:"authcode"`
	RedirectURI string `json:"redirect_uri"`
}

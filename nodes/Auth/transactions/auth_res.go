package internal

type AuthResponse struct {
	Authcode string `json:"authcode"`
	Redirect string `json:"redirect"`
}

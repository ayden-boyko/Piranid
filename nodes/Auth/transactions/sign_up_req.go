package internal

type SignUpRequest struct {
	Username       string `json:"username"`
	Useremail      string `json:"useremail"`
	HashedPassword string `json:"hashed_password"`
	ClientSecret   string `json:"client_secret"`
	Redirect       string `json:"redirect"`
	ClientId       string `json:"client_id"`
	ServiceId      string `json:"service_id"`
	RedirectURI    string `json:"redirect_uri"`
}

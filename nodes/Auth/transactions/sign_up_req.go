package internal

type SignUpRequest struct {
	Username       string `json:"username"`
	Useremail      string `json:"useremail"`
	HashedPassword string `json:"hashed_password"`
	ClientSecret   string `json:"client_secret"`
	Redirect       string `json:"redirect"`
}

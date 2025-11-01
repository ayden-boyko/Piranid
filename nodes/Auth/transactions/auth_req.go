package internal

type AuthRequest struct {
	Username     string `json:"username"`
	Useremail    string `json:"useremail"`
	Password     string `json:"password"`
	ClientId     string `json:"client_id"`
	RedirectURI  string `json:"redirect_uri"`
	Responsetype string `json:"response_type"`
}
